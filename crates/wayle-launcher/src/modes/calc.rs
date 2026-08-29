//! calc mode: the query is an expression, the first row is its result.
//!
//! rofi needs `rofi-calc` — a plugin, a C ABI, and `qalc` — for this. Here it
//! is a mode like any other: the expression is evaluated as you type, Enter
//! copies the result, and Shift+Enter puts it back in the input so the next
//! expression can build on it.
//!
//! The evaluator is deliberately small — arithmetic, parentheses, a handful of
//! functions and constants. It is not a computer algebra system and does not
//! do units or currency; `qalc` is still the answer for those, through a
//! script mode.

use async_trait::async_trait;

use crate::{
    item::{Item, ItemFlags},
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// Calculator mode.
pub struct CalcMode {
    /// The last successfully evaluated result, as shown.
    result: Option<String>,
}

impl Default for CalcMode {
    fn default() -> Self {
        Self::new()
    }
}

impl CalcMode {
    /// Create the mode.
    #[must_use]
    pub const fn new() -> Self {
        Self { result: None }
    }

    fn state(&self) -> ModeState {
        let items = match &self.result {
            // PERMANENT: the result is not a row the query is searching for,
            // it is the answer to the query. Matching "3" against "1+2" would
            // hide it the moment it appeared.
            Some(result) => vec![Item {
                display: format!("= {result}"),
                match_text: result.clone(),
                info: Some(result.clone()),
                flags: ItemFlags::PERMANENT,
                icon: None,
            }],
            None => Vec::new(),
        };
        ModeState {
            items,
            prompt: String::from("calc"),
            ..ModeState::default()
        }
    }
}

#[async_trait]
impl Mode for CalcMode {
    fn name(&self) -> &str {
        "calc"
    }

    async fn load(&mut self) -> ModeState {
        self.state()
    }

    fn query(&mut self, query: &str) -> Option<ModeState> {
        let result = eval(query).map(format_result);
        if result == self.result {
            return None;
        }
        self.result = result;
        Some(self.state())
    }

    async fn activate(&mut self, _index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        let Some(result) = self.result.clone() else {
            return Action::Nothing;
        };
        match kind {
            // Shift+Enter continues the calculation instead of ending it.
            ActivateKind::Alt => Action::SetInput(result),
            _ => Action::Copy(result),
        }
    }

    fn allows_custom(&self) -> bool {
        false
    }
}

/// Formats a result the way a person would write it: no trailing zeros, and
/// no `.0` on something that came out whole.
fn format_result(value: f64) -> String {
    if value == value.trunc() && value.abs() < 1e15 {
        return format!("{value:.0}");
    }
    let text = format!("{value:.10}");
    let trimmed = text.trim_end_matches('0').trim_end_matches('.');
    String::from(trimmed)
}

/// Evaluates an expression, or `None` when it is not one.
///
/// Incomplete input is not an error worth reporting: half of everything typed
/// into this mode is a half-finished expression, so a failure shows no row
/// rather than an error that flashes on every keystroke.
fn eval(input: &str) -> Option<f64> {
    let mut parser = Parser { rest: input.trim() };
    let value = parser.expression()?;
    parser.skip_spaces();
    if !parser.rest.is_empty() || !value.is_finite() {
        return None;
    }
    Some(value)
}

/// A recursive-descent parser over what is left of the expression.
struct Parser<'a> {
    rest: &'a str,
}

impl Parser<'_> {
    fn skip_spaces(&mut self) {
        self.rest = self.rest.trim_start();
    }

    /// Consumes `token` when it is next, reporting whether it was.
    fn eat(&mut self, token: char) -> bool {
        self.skip_spaces();
        match self.rest.strip_prefix(token) {
            Some(rest) => {
                self.rest = rest;
                true
            }
            None => false,
        }
    }

    fn peek(&mut self) -> Option<char> {
        self.skip_spaces();
        self.rest.chars().next()
    }

    /// `expression := term (('+' | '-') term)*`
    fn expression(&mut self) -> Option<f64> {
        let mut value = self.term()?;
        loop {
            match self.peek() {
                Some('+') => {
                    self.rest = &self.rest[1..];
                    value += self.term()?;
                }
                Some('-') => {
                    self.rest = &self.rest[1..];
                    value -= self.term()?;
                }
                _ => return Some(value),
            }
        }
    }

    /// `term := power (('*' | '/' | '%') power)*`
    fn term(&mut self) -> Option<f64> {
        let mut value = self.power()?;
        loop {
            match self.peek() {
                Some('*') => {
                    self.rest = &self.rest[1..];
                    value *= self.power()?;
                }
                Some('/') => {
                    self.rest = &self.rest[1..];
                    value /= self.power()?;
                }
                Some('%') => {
                    self.rest = &self.rest[1..];
                    value %= self.power()?;
                }
                _ => return Some(value),
            }
        }
    }

    /// `power := unary ('^' power)?` — right associative, as in maths and
    /// unlike every other operator here.
    fn power(&mut self) -> Option<f64> {
        let base = self.unary()?;
        if self.eat('^') {
            return Some(base.powf(self.power()?));
        }
        Some(base)
    }

    /// `unary := ('-' | '+') unary | atom`
    fn unary(&mut self) -> Option<f64> {
        match self.peek()? {
            '-' => {
                self.rest = &self.rest[1..];
                Some(-self.unary()?)
            }
            '+' => {
                self.rest = &self.rest[1..];
                self.unary()
            }
            _ => self.atom(),
        }
    }

    /// `atom := number | '(' expression ')' | name ['(' expression ')']`
    fn atom(&mut self) -> Option<f64> {
        if self.eat('(') {
            let value = self.expression()?;
            return self.eat(')').then_some(value);
        }
        match self.peek()? {
            digit if digit.is_ascii_digit() || digit == '.' => self.number(),
            letter if letter.is_ascii_alphabetic() => self.named(),
            _ => None,
        }
    }

    fn number(&mut self) -> Option<f64> {
        self.skip_spaces();
        let end = self
            .rest
            .find(|character: char| !character.is_ascii_digit() && character != '.')
            .unwrap_or(self.rest.len());
        let (digits, rest) = self.rest.split_at(end);
        self.rest = rest;
        digits.parse().ok()
    }

    /// A constant, or a function applied to a parenthesised argument.
    fn named(&mut self) -> Option<f64> {
        self.skip_spaces();
        let end = self
            .rest
            .find(|character: char| !character.is_ascii_alphabetic())
            .unwrap_or(self.rest.len());
        let (name, rest) = self.rest.split_at(end);
        self.rest = rest;

        match name {
            "pi" => return Some(std::f64::consts::PI),
            "e" => return Some(std::f64::consts::E),
            _ => {}
        }

        if !self.eat('(') {
            return None;
        }
        let argument = self.expression()?;
        if !self.eat(')') {
            return None;
        }
        Some(match name {
            "sqrt" => argument.sqrt(),
            "abs" => argument.abs(),
            "floor" => argument.floor(),
            "ceil" => argument.ceil(),
            "round" => argument.round(),
            "ln" => argument.ln(),
            "log" => argument.log10(),
            "sin" => argument.sin(),
            "cos" => argument.cos(),
            "tan" => argument.tan(),
            _ => return None,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn calc(input: &str) -> Option<String> {
        eval(input).map(format_result)
    }

    #[test]
    fn arithmetic_follows_precedence_and_parentheses() {
        assert_eq!(calc("1+2").as_deref(), Some("3"));
        assert_eq!(calc("2+3*4").as_deref(), Some("14"));
        assert_eq!(calc("(2+3)*4").as_deref(), Some("20"));
        assert_eq!(calc("10/4").as_deref(), Some("2.5"));
        assert_eq!(calc("10 % 3").as_deref(), Some("1"));
        assert_eq!(calc("2^10").as_deref(), Some("1024"));
        // Exponentiation is right-associative: 2^(3^2), not (2^3)^2.
        assert_eq!(calc("2^3^2").as_deref(), Some("512"));
        // Subtraction is not: (10-3)-2, not 10-(3-2).
        assert_eq!(calc("10-3-2").as_deref(), Some("5"));
    }

    #[test]
    fn signs_spaces_and_decimals() {
        assert_eq!(calc("-5+3").as_deref(), Some("-2"));
        assert_eq!(calc("3 - -2").as_deref(), Some("5"));
        assert_eq!(calc("  1   +   1  ").as_deref(), Some("2"));
        assert_eq!(calc("0.1+0.2").as_deref(), Some("0.3"));
        assert_eq!(calc(".5*4").as_deref(), Some("2"));
    }

    #[test]
    fn constants_and_functions() {
        assert_eq!(calc("sqrt(16)").as_deref(), Some("4"));
        assert_eq!(calc("round(pi*100)/100").as_deref(), Some("3.14"));
        assert_eq!(calc("abs(3-10)").as_deref(), Some("7"));
        assert_eq!(calc("log(1000)").as_deref(), Some("3"));
        assert_eq!(calc("floor(2.7)+ceil(0.2)").as_deref(), Some("3"));
    }

    #[test]
    fn half_typed_and_nonsense_input_is_not_a_result() {
        // Every one of these is a keystroke on the way to something valid, so
        // the mode has to stay quiet rather than error on each of them.
        for input in [
            "", "1+", "(1+2", "1+2)", "sqrt", "sqrt(", "sqrt(4", "*", "hello", "1 2", "1+*2",
            "nope(4)", "()",
        ] {
            assert_eq!(calc(input), None, "{input:?} must not evaluate");
        }
    }

    #[test]
    fn results_that_are_not_numbers_are_not_shown() {
        // Infinity and NaN would render as "inf"/"NaN", which is not an
        // answer anyone asked for.
        assert_eq!(calc("1/0"), None);
        assert_eq!(calc("0/0"), None);
        assert_eq!(calc("ln(-1)"), None);
    }

    #[test]
    fn whole_results_lose_their_decimal_point() {
        assert_eq!(format_result(3.0), "3");
        assert_eq!(format_result(-7.0), "-7");
        assert_eq!(format_result(2.5), "2.5");
        assert_eq!(format_result(1.0 / 3.0), "0.3333333333");
    }

    #[tokio::test]
    async fn the_result_row_survives_the_filter_that_would_hide_it() {
        let mut mode = CalcMode::new();
        let state = mode.query("1+2").expect("a new result reloads the list");
        assert_eq!(state.items.len(), 1);
        assert_eq!(state.items[0].display, "= 3");
        // Without PERMANENT the list is empty exactly when it has an answer.
        assert!(state.items[0].flags.contains(ItemFlags::PERMANENT));
    }

    #[tokio::test]
    async fn an_unfinished_expression_clears_the_previous_answer() {
        let mut mode = CalcMode::new();
        mode.query("1+2");
        let state = mode.query("1+").expect("losing the result reloads too");
        assert!(
            state.items.is_empty(),
            "a stale answer to a different expression is worse than none"
        );
    }

    #[tokio::test]
    async fn typing_that_does_not_change_the_answer_does_not_reload() {
        let mut mode = CalcMode::new();
        mode.query("1+2");
        // Same answer, different spelling: reloading would fight the user's
        // selection for nothing.
        assert!(mode.query("1 + 2").is_none());
    }

    #[tokio::test]
    async fn accepting_copies_and_shift_accepting_continues() {
        let mut mode = CalcMode::new();
        mode.query("6*7");
        let copied = mode.activate(Some(0), ActivateKind::Default, "6*7").await;
        assert!(matches!(copied, Action::Copy(result) if result == "42"));

        let continued = mode.activate(Some(0), ActivateKind::Alt, "6*7").await;
        assert!(matches!(continued, Action::SetInput(result) if result == "42"));
    }

    #[tokio::test]
    async fn accepting_nothing_does_nothing() {
        let mut mode = CalcMode::new();
        let action = mode.activate(None, ActivateKind::Default, "nonsense").await;
        assert!(matches!(action, Action::Nothing));
        // And typed text is never itself the answer.
        assert!(!mode.allows_custom());
    }
}

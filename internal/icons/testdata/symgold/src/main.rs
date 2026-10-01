#[allow(dead_code)]
#[path = "../../../../../crates/wayle-icons/src/transform.rs"]
mod transform;

// symgold DIR: for each DIR/*.svg write .golden (to_symbolic or NONE)
// and .parse (OK, or ERR: and usvg Tree::from_str's error).
fn main() {
    let dir = std::env::args().nth(1).expect("dir");
    for e in std::fs::read_dir(&dir).unwrap() {
        let p = e.unwrap().path();
        if p.extension().is_none_or(|x| x != "svg") { continue; }
        let Ok(src) = std::fs::read_to_string(&p) else { continue };
        let out = transform::to_symbolic(&src).unwrap_or_else(|| "NONE".into());
        std::fs::write(p.with_extension("svg.golden"), out).unwrap();
        let parse = match usvg::Tree::from_str(&src, &usvg::Options::default()) { Ok(_) => "OK".to_string(), Err(err) => format!("ERR: {err}") };
        std::fs::write(p.with_extension("svg.parse"), parse).unwrap();
    }
}

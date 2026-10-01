package greeter

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// uidMin and uidMax bound the regular-user range offered in the user
// list: below is system accounts, above reserved ranges and nobody.
const (
	uidMin = 1000
	uidMax = 59999
)

// accountsServiceIcons is where gdm and sddm read avatars from.
const accountsServiceIcons = "/var/lib/AccountsService/icons"

// User is one login account offered in the user list (users.rs).
type User struct {
	// Name is the login name greetd authenticates.
	Name string
	// DisplayName is the GECOS full name, else Name.
	DisplayName string
	// Home seeds ~/.face and cursor detection (it may be unreadable).
	Home string
	// Icon is a readable avatar path, "" when none.
	Icon string
}

// LoadUsers reads the login users from /etc/passwd, avatars resolved.
func LoadUsers() []User {
	data, _ := os.ReadFile("/etc/passwd")
	users := parsePasswd(string(data))
	for i := range users {
		users[i].Icon = findAvatar(users[i].Name, users[i].Home)
	}
	return users
}

// parsePasswd keeps regular-range accounts with a login shell, sorted
// by lowercased display name.
func parsePasswd(text string) []User {
	var users []User
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Split(line, ":")
		if len(f) != 7 {
			continue
		}
		uid, err := strconv.ParseUint(f[2], 10, 32)
		if err != nil || uid < uidMin || uid > uidMax || !isLoginShell(f[6]) {
			continue
		}
		full, _, _ := strings.Cut(f[4], ",")
		full = strings.TrimSpace(full)
		display := full
		if display == "" {
			display = f[0]
		}
		users = append(users, User{Name: f[0], DisplayName: display, Home: f[5]})
	}
	sort.SliceStable(users, func(i, j int) bool {
		return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
	})
	return users
}

// isLoginShell reports whether shell allows an interactive login.
func isLoginShell(shell string) bool {
	return shell != "" && !strings.HasSuffix(shell, "nologin") &&
		!strings.HasSuffix(shell, "/false") && shell != "/bin/sync"
}

// findAvatar returns the first readable avatar: the AccountsService
// icon, then ~/.face.
func findAvatar(name, home string) string {
	for _, candidate := range []string{filepath.Join(accountsServiceIcons, name), filepath.Join(home, ".face")} {
		if f, err := os.Open(candidate); err == nil { //nolint:gosec // avatar lookup
			_ = f.Close()
			return candidate
		}
	}
	return ""
}

package core

import (
	"fmt"
	"os"
	"os/user"
)

// UserContext holds resolved user information needed across packages.
type UserContext struct {
	Username string // Pre-sanitized for label use
	UID      string
	HomeDir  string
}

// CurrentUser resolves the current user context with the system home dir.
func CurrentUser() (UserContext, error) {
	return CurrentUserWithHome("")
}

// CurrentUserWithHome resolves the current user context, using homeOverride
// if non-empty (useful for testing).
func CurrentUserWithHome(homeOverride string) (UserContext, error) {
	u, err := user.Current()
	if err != nil {
		return UserContext{}, fmt.Errorf("current user: %w", err)
	}
	home := homeOverride
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return UserContext{}, fmt.Errorf("user home: %w", err)
		}
	}
	return UserContext{
		Username: SanitizeLabelPart(u.Username),
		UID:      u.Uid,
		HomeDir:  home,
	}, nil
}

package guestconfig

import (
	"errors"
	"math"
	"regexp"
)

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

type Account struct {
	Username string `json:"username"`
	UID      uint64 `json:"uid"`
}

func (a Account) Validate() error {
	if !usernamePattern.MatchString(a.Username) || a.Username == "root" {
		return errors.New("microvm.username must be a non-root Linux username (1-32 lowercase letters, digits, underscores or hyphens, starting with a letter or underscore)")
	}
	if a.UID == 0 || a.UID >= math.MaxUint32 {
		return errors.New("microvm.uid must be between 1 and 4294967294")
	}
	return nil
}

func (a Account) Home() string { return "/home/" + a.Username }

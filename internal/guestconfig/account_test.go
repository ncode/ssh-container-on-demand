package guestconfig

import (
	"math"
	"strings"
	"testing"
)

func TestAccountValidate(t *testing.T) {
	for _, test := range []struct {
		name    string
		account Account
		valid   bool
	}{
		{"default", Account{Username: "shell", UID: 1000}, true},
		{"custom", Account{Username: "operator_2", UID: 1234}, true},
		{"empty", Account{Username: "", UID: 1000}, false},
		{"root", Account{Username: "root", UID: 1000}, false},
		{"path", Account{Username: "../etc", UID: 1000}, false},
		{"separator", Account{Username: "user:x", UID: 1000}, false},
		{"long", Account{Username: strings.Repeat("a", 33), UID: 1000}, false},
		{"zero UID", Account{Username: "shell", UID: 0}, false},
		{"max32 UID", Account{Username: "shell", UID: math.MaxUint32}, false},
		{"max64 UID", Account{Username: "shell", UID: math.MaxUint64}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.account.Validate()
			if (err == nil) != test.valid {
				t.Fatalf("Validate(%+v) = %v, valid = %v", test.account, err, test.valid)
			}
		})
	}
}

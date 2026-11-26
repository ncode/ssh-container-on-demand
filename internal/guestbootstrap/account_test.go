package main

import (
	"testing"

	"github.com/ncode/ssh-container-on-demand/internal/guestconfig"
)

func TestWithGuestRecord(t *testing.T) {
	account := guestconfig.Account{Username: "operator", UID: 1234}
	data := "root:x:0:0:root:/root:/bin/sh\nold:x:01234:1234::/home/old:/bin/sh\noperator:x:2000:2000::/elsewhere:/bin/sh\nnobody:x:65534:65534::/:/sbin/nologin"
	record := "operator:x:1234:1234::/home/operator:/bin/sh"
	want := "root:x:0:0:root:/root:/bin/sh\nnobody:x:65534:65534::/:/sbin/nologin\n" + record + "\n"
	if got := withGuestRecord(data, record, account); got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	if got := withGuestRecord("", "operator:x:1234:", account); got != "operator:x:1234:\n" {
		t.Fatalf("empty file = %q", got)
	}
}

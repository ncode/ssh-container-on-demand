package main

import "testing"

func TestWithGuestRecord(t *testing.T) {
	data := "root:x:0:0:root:/root:/bin/sh\nold:x:01000:1000::/home/old:/bin/sh\nguest:x:2000:2000::/elsewhere:/bin/sh\nnobody:x:65534:65534::/:/sbin/nologin"
	record := "guest:x:1000:1000::/home/guest:/bin/sh"
	want := "root:x:0:0:root:/root:/bin/sh\nnobody:x:65534:65534::/:/sbin/nologin\n" + record + "\n"
	if got := withGuestRecord(data, record); got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	if got := withGuestRecord("", "guest:x:1000:"); got != "guest:x:1000:\n" {
		t.Fatalf("empty file = %q", got)
	}
}

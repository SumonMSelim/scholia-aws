package auth

import "testing"

func TestGuestSubject(t *testing.T) {
	a, err := NewGuestSubject()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewGuestSubject()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != len(GuestPrefix)+32 || !IsGuest(a) {
		t.Fatalf("guest subjects %q %q", a, b)
	}
	for _, subject := range []string{"", "local-owner", "4f1c-guest-x"} {
		if IsGuest(subject) {
			t.Fatalf("IsGuest(%q) = true", subject)
		}
	}
}

package wire

import "testing"

func TestTargets(t *testing.T) {
	good := []string{"127.0.0.1", "localhost", "example.com", "1.1.1.1", "::1", "2001:db8::1"}
	bad := []string{"-n 99", "127.0.0.1;whoami", "a & calc", "a\nb", "https://example.com", "..", "foo..bar", "foo.-bar", ""}
	for _, x := range good {
		if !ValidTarget(x) {
			t.Errorf("rejected %q", x)
		}
	}
	for _, x := range bad {
		if ValidTarget(x) {
			t.Errorf("accepted %q", x)
		}
	}
}
func TestBootstrap(t *testing.T) {
	b := Bootstrap{2, "http://127.0.0.1:18777", Token()}
	out, e := Decode(b.Encode())
	if e != nil || out != b {
		t.Fatal(out, e)
	}
	for _, s := range []string{"http://8.8.8.8", "http://example.com", "https://user:secret@example.com", "https://example.com/?ticket=x", "javascript:hello"} {
		if ValidateBase(s) == nil {
			t.Fatal("accepted", s)
		}
	}
	for _, s := range []string{"http://10.1.2.3:18777", "http://172.22.9.38:18777", "http://192.168.1.2", "http://[fd00::1]:18777", "https://support.example.com"} {
		if ValidateBase(s) != nil {
			t.Fatal("rejected", s)
		}
	}
}

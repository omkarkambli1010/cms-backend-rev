package auth

import "testing"

// The vector was produced by the Node implementation this service replaces
// (crypto.scryptSync(password, salt, 64)), so existing hashes keep verifying.
func TestVerifyPasswordMatchesNode(t *testing.T) {
	const hash = "43ee0953d16a944a392e32f629f05ce8170ae16750a6f70c67332ee0c7b8f65382c7654a1ca6995c778ed4fd871d4a2ad35cc6a878d7b14d1ad0b8edbb079ba7"
	if !VerifyPassword("correct horse battery", hash, "abc123salt") {
		t.Fatal("hash from the Node implementation did not verify")
	}
	if VerifyPassword("wrong", hash, "abc123salt") {
		t.Fatal("wrong password verified")
	}
}

func TestThrottle(t *testing.T) {
	th := NewThrottle()
	for i := 0; i < maxAttempts; i++ {
		if !th.Hit("k") {
			t.Fatalf("attempt %d refused early", i+1)
		}
	}
	if th.Hit("k") {
		t.Fatal("attempt over the allowance was allowed")
	}
	th.Clear("k")
	if !th.Hit("k") {
		t.Fatal("cleared key still blocked")
	}
}

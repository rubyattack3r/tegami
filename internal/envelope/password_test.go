package envelope

import (
	"regexp"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	shape := regexp.MustCompile(`^[a-km-np-z2-9]{5}(-[a-km-np-z2-9]{5}){3}$`)
	seen := map[string]bool{}
	for range 100 {
		password, err := GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if !shape.MatchString(password) || validatePassword([]byte(password)) != nil {
			t.Fatalf("generated password %q is malformed", password)
		}
		if seen[password] {
			t.Fatalf("generated password %q twice", password)
		}
		seen[password] = true
	}
}

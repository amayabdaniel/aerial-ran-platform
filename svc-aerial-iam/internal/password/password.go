// Package password is a thin bcrypt wrapper with one knob (cost).
package password

import "golang.org/x/crypto/bcrypt"

const defaultCost = 12

// Hash returns a bcrypt hash of p.
func Hash(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), defaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Check returns nil if p matches the stored hash.
func Check(hash, p string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p))
}

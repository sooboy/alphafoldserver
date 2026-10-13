package platform

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func passwordHash(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", fail("PASSWORD_LENGTH_12_TO_256")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}
func passwordOK(encoded, password string) bool {
	if len(password) > 256 {
		return false
	}
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := hex.DecodeString(parts[1])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
func tokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return hex.EncodeToString(v[:])
}
func CreateUser(s *Store, name, password, role, actor string) (User, error) {
	if !validID.MatchString(name) || (role != "admin" && role != "member") {
		return User{}, fail("INVALID_USER")
	}
	hash, err := passwordHash(password)
	if err != nil {
		return User{}, err
	}
	user := User{ID: ID(), Name: name, Role: role, PasswordHash: hash}
	err = s.Update(func(st *State) error {
		for _, u := range st.Users {
			if strings.EqualFold(u.Name, name) {
				return fail("USER_EXISTS")
			}
		}
		st.Users[user.ID] = user
		st.log(actor, "user.create", user.ID)
		return nil
	})
	return user, err
}

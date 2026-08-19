package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Role         Role   `json:"role"`
}

type UserStore struct {
	path string
	enc  encryptFunc
	mu   sync.RWMutex
	users []User
}

type encryptFunc interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

func NewUserStore(basePath string, enc encryptFunc) (*UserStore, error) {
	us := &UserStore{
		path: filepath.Join(basePath, "users.enc"),
		enc:  enc,
	}
	if err := us.load(); err != nil {
		return nil, err
	}
	return us, nil
}

func (us *UserStore) load() error {
	data, err := os.ReadFile(us.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	plain, err := us.enc.Decrypt(data)
	if err != nil {
		return fmt.Errorf("decrypt users: %w", err)
	}
	return json.Unmarshal(plain, &us.users)
}

func (us *UserStore) save() error {
	data, err := json.Marshal(us.users)
	if err != nil {
		return err
	}
	enc, err := us.enc.Encrypt(data)
	if err != nil {
		return err
	}
	return os.WriteFile(us.path, enc, 0600)
}

func (us *UserStore) Bootstrap(username, password string, role Role) error {
	us.mu.Lock()
	defer us.mu.Unlock()
	if len(us.users) > 0 {
		return fmt.Errorf("users already exist")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	us.users = []User{{Username: username, PasswordHash: string(hash), Role: role}}
	return us.save()
}

func (us *UserStore) Authenticate(username, password string) (*User, error) {
	us.mu.RLock()
	defer us.mu.RUnlock()
	for _, u := range us.users {
		if u.Username == username {
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
				return nil, fmt.Errorf("invalid credentials")
			}
			return &u, nil
		}
	}
	return nil, fmt.Errorf("invalid credentials")
}

func (us *UserStore) List() []User {
	us.mu.RLock()
	defer us.mu.RUnlock()
	out := make([]User, len(us.users))
	for i, u := range us.users {
		out[i] = User{Username: u.Username, Role: u.Role}
	}
	return out
}

func (us *UserStore) Create(username, password string, role Role) error {
	us.mu.Lock()
	defer us.mu.Unlock()
	for _, u := range us.users {
		if u.Username == username {
			return fmt.Errorf("user exists")
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	us.users = append(us.users, User{Username: username, PasswordHash: string(hash), Role: role})
	return us.save()
}

func (us *UserStore) Delete(username string) error {
	us.mu.Lock()
	defer us.mu.Unlock()
	var filtered []User
	found := false
	for _, u := range us.users {
		if u.Username == username {
			found = true
			continue
		}
		filtered = append(filtered, u)
	}
	if !found {
		return fmt.Errorf("user not found")
	}
	if len(filtered) == 0 {
		return fmt.Errorf("cannot delete last user")
	}
	us.users = filtered
	return us.save()
}

func (us *UserStore) Count() int {
	us.mu.RLock()
	defer us.mu.RUnlock()
	return len(us.users)
}

func (us *UserStore) Get(username string) (*User, error) {
	us.mu.RLock()
	defer us.mu.RUnlock()
	for _, u := range us.users {
		if u.Username == username {
			copy := u
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func RoleCanDeploy(r Role) bool {
	return r == RoleAdmin || r == RoleOperator
}

func RoleCanManageUsers(r Role) bool {
	return r == RoleAdmin
}

func RoleCanWrite(r Role) bool {
	return r == RoleAdmin || r == RoleOperator
}

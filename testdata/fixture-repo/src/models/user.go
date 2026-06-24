package models

import "time"

// User represents an application account.
type User struct {
	ID        string
	Email     string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Active    bool
}

// UserRepository persists and loads users.
type UserRepository interface {
	FindByID(id string) (*User, error)
	FindByEmail(email string) (*User, error)
	Save(user *User) error
}

// NewUser constructs a user with sane defaults.
func NewUser(id, email, name string) *User {
	now := time.Now()
	return &User{
		ID:        id,
		Email:     email,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		Active:    true,
	}
}

// Deactivate marks the user inactive without deleting history.
func Deactivate(user *User) {
	user.Active = false
	user.UpdatedAt = time.Now()
}

// DisplayName returns a friendly label for UI surfaces.
func DisplayName(user *User) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Email
}

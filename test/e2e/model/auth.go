package model

import "github.com/golang-jwt/jwt/v5"

type User struct {
	Username     string
	Organization string
	EmailDomain  string
	FirstName    string
	LastName     string
	Token        *jwt.Token
}

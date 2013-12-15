package pwauth

import (
	"code.google.com/p/go.crypto/ssh"
	"fmt"
	"github.com/howeyc/gopass"
)

type PasswordPrompt struct {
	UserName string
	Host string
	password string
}

func ClientAuthPrompt(userName, host string) ssh.ClientAuth {
	return ssh.ClientAuthPassword(NewPasswordPrompt(userName, host))
}

func NewPasswordPrompt(userName, host string) PasswordPrompt {
	return PasswordPrompt{userName, host, ""}
}

func (p PasswordPrompt) Password(userName string) (string, error) {
	if userName != "" {
		p.UserName = userName
	}
	if p.password == "" {
		fmt.Printf("%s@%s's password:", p.UserName, p.Host)
		p.password = string(gopass.GetPasswd())
	}
	return p.password, nil
}


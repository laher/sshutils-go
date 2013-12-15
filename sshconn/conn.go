package sshconn

import (
	"code.google.com/p/go.crypto/ssh"
	"fmt"
	"github.com/laher/sshutils-go/sshagent"
	"github.com/laher/sshutils-go/keyring"
	"github.com/laher/sshutils-go/knownhosts"
	"github.com/laher/sshutils-go/pwauth"
	"os"
	"os/user"
	"runtime"
	"strings"
)

func FillDefaultUsername(userName string) string {
	if userName == "" { //check 
		u, err := user.Current()
		if err != nil {
			//never mind (probably cross-compiled. $USER usually does the trick)
			userName = os.Getenv("USER")
		} else {
			userName = u.Username
			//remove 'domain'
			if runtime.GOOS == "windows" && strings.Contains(userName, "\\") {
				parts := strings.Split(userName, "\\")
				userName = parts[1]
			}
		}
	}
	return userName
}

func Connect(userName, host string, port int, idFile string, checkKnownHosts bool, verbose bool) (*ssh.Session, error) {
	auths := []ssh.ClientAuth{}
	userName = FillDefaultUsername(userName)
	if idFile != "" {
		auth, err := keyring.LoadKeyring(idFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading key file (%v)\n", err)
		} else {
			auths = append ( auths, auth)
		}
	} else {
		auth, err := sshagent.AgentClientDefault()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting agent (%v)\n", err)
		} else {
			auths = append ( auths, auth)
		}
	}
	auth := pwauth.ClientAuthPrompt(userName, host)
	auths = append (auths, auth )
	clientConfig := &ssh.ClientConfig{
		User: userName,
		Auth: auths,
	}
	if checkKnownHosts {
		clientConfig.HostKeyChecker = knownhosts.LoadKnownHosts(verbose)
	}
	target := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", target, clientConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to dial: "+err.Error())
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create session: "+err.Error())
	}
	return session, err

}

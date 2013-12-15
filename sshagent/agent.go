package sshagent

import(
	"code.google.com/p/go.crypto/ssh"
	"errors"
	"net"
	"os"
)


func AgentClientDefault() (ssh.ClientAuth, error) {
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock != "" {
		return AgentClient(sshAuthSock)
	} else {
		return nil, errors.New("Did not load ssh-agent because SSH_AUTH_SOCK not available.")
	}

}

func AgentClient(address string) (ssh.ClientAuth, error) {
	agentClient, err := net.Dial("unix", address)
	if err != nil {
		return nil, err
	} else {
		return ssh.ClientAuthAgent(ssh.NewAgentClient(agentClient)), nil
	}
}

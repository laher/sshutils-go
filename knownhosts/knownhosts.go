package knownhosts

import (
	"bufio"
	"bytes"
	"code.google.com/p/gosshold/ssh"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"text/template"
)

type KnownHostsKeyAdder interface {
	AddHost(host string, algorithm string, key []byte, verbose bool) error
}

type KnownHostsKeyChecker struct {
	KnownHosts   map[string][]byte
	RevokedHosts map[string][]byte
	CAHosts      map[string][]byte
	verbose      bool
	KeyAdder KnownHostsKeyAdder
	errPipe	     io.Writer
}

type KnownHostsKeyAdderNoop struct {
}

func (kan KnownHostsKeyAdderNoop) AddHost(host string, algorithm string, key []byte, verbose bool) error {
	return errors.New("Key not found for "+host+". 'add key' not implemented yet in scp-go")
}

type KnownHostsKeyAdderPrompt struct {

}

func (kan KnownHostsKeyAdderPrompt) AddHost(host string, algorithm string, key []byte, verbose bool) error {
	fmt.Printf("Key not found for host %s. Accept? [Y/n]\n", host)
	confirm := ""
	fmt.Scanf("%s", &confirm)
	if confirm == "" || confirm == "y" || confirm == "Y" {
		return AddKnownHost(host, algorithm, key, true)
	} else {
		return errors.New("Not adding key or connecting")
	}
}

func checkHashedHost(knownHost string, host string) error {
	if strings.HasPrefix(knownHost, "|1|") {
		parts := strings.Split(knownHost, "|")
		if len(parts) > 3 {
			salt := parts[2]
			knownHash := parts[3]
			//hash check
			saltDecoded, err := base64.StdEncoding.DecodeString(salt)
			//salt decoded
			if err != nil {
				return err
			}
			h := hmac.New(sha1.New, saltDecoded)
			_, err = h.Write([]byte(host))
			if err != nil {
				return err
			}
			out := h.Sum(nil)
			hashed := base64.StdEncoding.EncodeToString(out)
			if hashed == knownHash {
				return nil
			} else {
				//ignore line
			}
		} else {
			fmt.Printf("Invalid hashed host line\n")
		}
	} else {
		fmt.Printf("host line not hashed\n")
	}
	return errors.New("Not matched")
}
func parseWireKey(bs []byte, verbose bool) ssh.PublicKey {
	pk, rest, ok := ssh.ParsePublicKey(bs)
	if verbose {
		fmt.Printf("rest: %v, ok: %v\n", rest, ok)
	}
	return pk
}

func readHostFileKey(bs []byte, verbose bool) ssh.PublicKey {
	pk, comment, options, rest, ok := ssh.ParseAuthorizedKey(bs)
	if verbose {
		fmt.Printf("comment: %s, options: %v, rest: %v, ok: %v\n", comment, options, rest, ok)
	}
	return pk
}

func (khkc KnownHostsKeyChecker) IsRevoked(hostPKWireFormat []byte) error {
	for k, existingKey := range khkc.RevokedHosts {
		existingPublicKey := readHostFileKey(existingKey, khkc.verbose)
		existingPKWireFormat := existingPublicKey.Marshal()
		if bytes.Equal(hostPKWireFormat, existingPKWireFormat) {
			return errors.New("Key has been revoked (as host '"+k+"')")
		}
	}
	return nil
}


func (khkc KnownHostsKeyChecker) matchHostWithHashSupport(host string) ([]byte, error) {
	existingKey, hostFound := khkc.KnownHosts[host]
	if !hostFound {
		//check by hash
		for k, v := range khkc.KnownHosts {
			err := checkHashedHost(k, host)
			if err != nil {
				//not matching
				//fmt.Printf("checkHashedHost failed")
			} else {
				//, v, hostKey)
				return v, nil
			}
		}
	} else {
		return existingKey, nil
	}
	return nil, errors.New("Not found")
}
func (khkc KnownHostsKeyChecker) Check(addr string, remote net.Addr, algorithm string, hostKey []byte) error {
	hostPublicKey := parseWireKey(hostKey, khkc.verbose)
	hostPKWireFormat := hostPublicKey.Marshal()
	err := khkc.IsRevoked(hostPKWireFormat)
	if err != nil {
		return err
	}
	remoteAddr := remote.String()
	hostport := strings.SplitN(remoteAddr, ":", 2)
	host := hostport[0]
	existingKey, err := khkc.matchHostWithHashSupport(host)
	if err != nil {
		err = khkc.KeyAdder.AddHost(host, algorithm, hostKey, khkc.verbose)
		if err != nil {
			return err
		}
		//load again
		existingKey, err = khkc.matchHostWithHashSupport(host)
		if err != nil {
			return err
		}
	}
	existingPublicKey := readHostFileKey(existingKey, khkc.verbose)
	existingPKWireFormat := existingPublicKey.Marshal()
	if bytes.Equal(hostPKWireFormat, existingPKWireFormat) {
		if khkc.verbose {
			fmt.Printf("OK keys match\n")
		}
		return nil
	} else {
tpl := `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be eavesdropping on you right now (man-in-the-middle attack)!
It is also possible that a host key has just been changed.
The fingerprint for the {{.HostKeyType}} key sent by the remote host is
{{.HostKeyFingerprint}}.
Please contact your system administrator.
Add correct host key in {{.KnownHostsFile}} to get rid of this message.
Offending {{.HostKeyType}} key in {{.KnownHostsFile}}:{{.Line}}
  remove with: ssh-keygen -f "{{.KnownHostsFile}}" -R {{.Host}}
{{.HostKeyType}} host key for {{.Host}} has changed and you have requested strict checking.
`
		t := template.Must(template.New("error").Parse(tpl))
		r := struct {
			HostKeyType string
			HostKeyFingerprint string
			KnownHostsFile string
			Host string
			Line int
		} {
			"ECDSA",
			"XX:XX:XX:XX",
			"/blah/file",
			host,
			1,
		}
		err := t.Execute(khkc.errPipe, r)
		if err != nil {
			fmt.Fprintf(khkc.errPipe, "Error generating error message: %v\n", err)
		}
		return errors.New("Host key verification failed")
	}
}

func AddKnownHost(host string, algorithm string, key []byte, verbose bool) error {
	file, err := OpenKnownHostsWriter(verbose)
	if err != nil {
		return err
	}
	keyEncoded := base64.StdEncoding.EncodeToString(key)
	_, err = file.WriteString(fmt.Sprintf("%s %s %s\n", host, algorithm, keyEncoded))
	if err != nil {
		return err
	}
	return file.Close()
}

func OpenKnownHostsWriter(verbose bool) (*os.File, error) {
	sshDir := filepath.Join(userHomeDir(verbose), ".ssh")
	_, err := os.Stat(sshDir)
	if os.IsNotExist(err) {
		if verbose {
			fmt.Printf("%s does not exist. Create\n", sshDir)
		}
		err := os.Mkdir(sshDir, 0700)
		if err != nil {
			fmt.Printf("Could not create %s\n", sshDir)
			return nil, err
		}
	}
	knownHostsFile := filepath.Join(sshDir, "known_hosts")
	flags := os.O_RDWR|os.O_APPEND
	_, err = os.Stat(knownHostsFile)
	if os.IsNotExist(err) {
		if verbose {
			fmt.Printf("%s does not exist. Create\n", knownHostsFile)
		}
		flags = flags|os.O_CREATE
	} else if err != nil {
		return nil, err
	}
	return os.OpenFile(knownHostsFile, flags, 0600)
}

func LoadKnownHosts(verbose bool, errPipe io.Writer) KnownHostsKeyChecker {
	knownHosts := map[string][]byte{}
	revokedHosts := map[string][]byte{}
	caHosts := map[string][]byte{}
	khkc := KnownHostsKeyChecker{knownHosts, revokedHosts, caHosts, verbose, KnownHostsKeyAdderPrompt{}, errPipe}
	sshDir := filepath.Join(userHomeDir(verbose), ".ssh")
	_, err := os.Stat(sshDir)
	if os.IsNotExist(err) {
		fmt.Printf("%s does not exist\n", sshDir)
		err := os.Mkdir(sshDir, 0700)
		if err != nil {
			fmt.Printf("Could not create %s\n", sshDir)
		}
		return khkc
	}
	knownHostsFile := filepath.Join(sshDir, "known_hosts")
	_, err = os.Stat(knownHostsFile)
	if os.IsNotExist(err) {
		fmt.Printf("%s does not exist\n", knownHostsFile)
		return khkc
	}
	file, err := os.Open(knownHostsFile)
	if err != nil {
		fmt.Printf("Could not open %s\n", knownHostsFile)
		return khkc
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			//ignore
		} else {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				//check for revoked / ca / ...
				isRevoked := false
				isCa := false
				if parts[0] == "@revoked" {
					isRevoked = true
					parts = strings.SplitN(parts[1], " ", 2)
				}
				if parts[0] == "@cert-authority" {
					isCa = true
					parts = strings.SplitN(parts[1], " ", 2)
				}

				if verbose {
					fmt.Printf("Known host %s, type: %s\n", parts[0], parts[1]) // Println will add back the final '\n'
				}
				pk, comment, options, rest, ok := ssh.ParseAuthorizedKey([]byte(parts[1]))
				if ok {
					if verbose {
						fmt.Printf("OK known host key for %s |%s| comment: %s, options: %v, rest: %v\n", parts[0], base64.StdEncoding.EncodeToString(pk.Marshal()), comment, options, rest)
					}
					if verbose {
						fmt.Printf("Setting %s = %s\n", parts[0], parts[1])
					}
					if isRevoked {
						revokedHosts[parts[0]] = []byte(parts[1])
					} else if isCa {
						caHosts[parts[0]] = []byte(parts[1])
					} else {
						knownHosts[parts[0]] = []byte(parts[1])
					}
				} else {
					fmt.Printf("Could not decode hostkey %s\n", parts[1])
				}
			} else {
				fmt.Printf("Unparseable host %s\n", line)
			}
		}
	}
	return khkc
}

func userHomeDir(verbose bool) string {
	usr, err := user.Current()
	if err != nil {
		fmt.Printf("Could not get home directory: %s\n", err)
		return os.Getenv("HOME")
	}
	if verbose {
		fmt.Printf("user dir: %s\n", usr.HomeDir)
	}
	return usr.HomeDir

}

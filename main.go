package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/avast/retry-go"
	"github.com/golang-jwt/jwt/v5"
)

const DataPath = "/data"
const Key = "You Key"

type Config struct {
	Host     string
	Port     string
	Password string
}

func loadConfig() (Config, error) {
	f, err := os.Open(DataPath + "/config.json")
	if err != nil {
		return Config{}, err
	}
	s := struct {
		Addr     string `json:"bind"`
		Password string `json:"setup_password"`
	}{}
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return Config{}, err
	}

	if s.Password == "" || s.Addr == "" {
		return Config{}, errors.New("bind or setup_password is empty, cannot reload server")
	}
	host, port, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return Config{}, fmt.Errorf("setup_password err: %w", err)
	}

	return Config{
		Host:     host,
		Port:     port,
		Password: s.Password,
	}, nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		panic(err)
	}

	fn := func() (bool, error) {
		ip, err := GetIPs()
		if err != nil {
			return false, fmt.Errorf("get ip err: %w\n", err)
		}
		ip2, err := readIPFromFile()
		if err != nil {
			log.Printf("read ip err: %v\n", err)
		}
		if ip == ip2 {
			return false, nil
		}
		log.Printf("ip changed: %v -> %v\n", ip2, ip)
		license, err := GetLicense()
		if err != nil {
			return false, fmt.Errorf("get license err: %w\n", err)
		}
		log.Printf("get new license: %s\n", license)
		if err := WriteLicense(license); err != nil {
			return false, fmt.Errorf("write license err: %w\n", err)
		}
		// Finally update the address file to prevent the license from failing to get,
		// and the address is successfully updated, which causes the retry
		// to think that the address has not changed.
		if err := writeIPToFile(ip); err != nil {
			return false, fmt.Errorf("write ip err: %w\n", err)
		}
		return true, nil
	}

	if _, err := fn(); err != nil {
		panic(err)
	}
	go func() {
		for {
			if err := Run(); err != nil {
				log.Printf("server terminate, err: %+v\n", err)
			}
		}
	}()

	for {
		time.Sleep(3 * time.Minute)
		b, err := fn()
		if err != nil {
			log.Printf("err: %v\n", err)
			continue
		}
		if b {
			time.Sleep(10 * time.Second) // waiting for the license to take effect.
			err := retry.Do(func() error {
				return config.ReloadServer()
			})
			if err != nil {
				log.Printf("reload server err: %v\n", err)
			}
		}
	}
}

func GetLicense() (string, error) {
	url := fmt.Sprintf("https://dash.pandoranext.com/data/%s/license.jwt", Key)
	r, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}

	s := string(bytes.TrimSpace(b))
	if _, _, err := jwt.NewParser().ParseUnverified(s, jwt.MapClaims{}); err != nil {
		return "", fmt.Errorf("parse jwt: %s err: %w", b, err)
	}
	return s, nil
}

func WriteLicense(license string) error {
	f, err := os.OpenFile(DataPath+"/license.jwt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(license)
	return err
}

func Run() error {
	c := exec.Command(
		`/opt/app/PandoraNext`,
		"-config", DataPath+"/config.json",
		"-tokens", DataPath+"/tokens.json",
		"-license", DataPath+"/license.jwt",
	)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

type IPAddr struct {
	V4 string `json:"v4"`
	V6 string `json:"v6"`
}

// GetIPs retrieves both IPv4 and IPv6 addresses.
// If only IPv4 is supported, the V6 field will also contain the IPv4 address.
func GetIPs() (IPAddr, error) {
	v4URL := "https://api.ipify.org?format=text"
	r, err := http.Get(v4URL)
	if err != nil {
		return IPAddr{}, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return IPAddr{}, err
	}
	v4 := string(b)

	v6URL := "https://api64.ipify.org?format=text"
	r, err = http.Get(v6URL)
	if err != nil {
		return IPAddr{}, err
	}
	defer r.Body.Close()
	b, err = io.ReadAll(r.Body)
	if err != nil {
		return IPAddr{}, err
	}
	v6 := string(b)

	return IPAddr{
		V4: v4,
		V6: v6,
	}, nil
}

func readIPFromFile() (IPAddr, error) {
	f, err := os.OpenFile("/opt/app/ip.txt", os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return IPAddr{}, err
	}

	i := IPAddr{}
	if err := json.NewDecoder(f).Decode(&i); err != nil {
		return IPAddr{}, err
	}
	return i, nil
}

func writeIPToFile(i IPAddr) error {
	f, err := os.OpenFile("/opt/app/ip.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(i)
}

func (c *Config) ReloadServer() error {
	if c.Password == "" || c.Port == "" {
		return errors.New("password or bind is empty, cannot reload server")
	}

	var addr string
	if c.Host == "" || c.Host == "0.0.0.0" || c.Host == "::" {
		addr = "localhost:" + c.Port
	} else {
		addr = c.Host + ":" + c.Port
	}

	r, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/setup/reload", addr), nil)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+c.Password)
	r2, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, r2.Body)
		r2.Body.Close()
	}()

	if r2.StatusCode != http.StatusOK {
		return fmt.Errorf("reload server: %s", r2.Status)
	}
	return nil
}

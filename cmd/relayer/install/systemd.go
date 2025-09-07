package install

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/RogueTeam/relayer/cmd/relayer/run"
	"gopkg.in/yaml.v3"
)

//go:embed systemd.service
var systemdService []byte

func isSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}

	return false
}

const (
	configDirectory = "/etc/relayer"
	configFile      = configDirectory + "/config.yaml"
)

func installSystemd() (err error) {

	// Create user
	const username = "relayer"
	exists, err := userExists(username)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	if !exists {
		log.Printf("User %s doesn't exists", username)
		err = exec.Command("/sbin/useradd", "-m", username).Run()
		if err != nil {
			return fmt.Errorf("failed to create username: %w", err)
		}
	} else {
		log.Printf("User %s already exists", username)
	}

	// Prepare executable
	log.Println("Preparing Binary")
	execContents, err := os.ReadFile(os.Args[0])
	if err != nil {
		return fmt.Errorf("failed to read executable contents: %w", err)
	}

	err = os.WriteFile("/usr/bin/relayer", execContents, 0o755)
	if err != nil {
		return fmt.Errorf("failed to write executable: %w", err)
	}

	// Prepare configuration location
	log.Println("Preparing configuration directory")
	os.MkdirAll(configDirectory, 0o755)
	config, err := yaml.Marshal(run.Example)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	_, err = os.Stat(configFile)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("failed to get file stat: %w", err)
		}
		log.Println("Writing new configuration")
		err = os.WriteFile(configFile, config, 0o755)
		if err != nil {
			return fmt.Errorf("failed to write example configuration: %w", err)
		}
	} else {
		log.Println("Skipping configuration. Already exists")
	}

	// Enable
	log.Println("chown", "-R", "root:relayer", configDirectory)
	err = exec.Command("chown", "-R", "root:relayer", "/etc/relayer").Run()
	if err != nil {
		return fmt.Errorf("failed to change ownership of configuration: %w", err)
	}

	// Write service file
	log.Println("Writing service file")
	err = os.WriteFile("/etc/systemd/system/relayer.service", systemdService, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write relayer service: %w", err)
	}

	log.Println("Reload service daemon")
	err = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
	if err != nil {
		return fmt.Errorf("failed to reload daemon: %w", err)
	}

	log.Println("Restart service")
	err = exec.Command("/usr/bin/systemctl", "restart", "relayer").Run()
	if err != nil {
		return fmt.Errorf("failed to restart servce: %w", err)
	}
	return nil
}

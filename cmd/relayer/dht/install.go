package dht

import (
	"fmt"
	"log"
	"os"

	"github.com/RogueTeam/relayer/internal/system"
	"gopkg.in/yaml.v3"
)

const (
	configDirectory = "/etc/relayer-dht"
	configFile      = configDirectory + "/config.yaml"
)

const serviceFile = "/etc/systemd/system/relayer-dht.service"
const binaryPath = "/usr/bin/relayer"
const serviceName = "relayer-dht"
const username = "relayer-dht"

func install(svcHandler system.ServiceHandler, servicefile []byte) (err error) {
	log.Println("Stopping live service")
	svcHandler.Stop(serviceName)

	// Create user
	exists, err := system.UserExists(username)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	if !exists {
		log.Printf("User %s doesn't exists", username)
		err = system.CreateUserWithHome(username)
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

	err = os.WriteFile(binaryPath, execContents, 0o755)
	if err != nil {
		return fmt.Errorf("failed to write executable: %w", err)
	}

	// Prepare configuration location
	log.Println("Preparing configuration directory")
	os.MkdirAll(configDirectory, 0o755)
	config, err := yaml.Marshal(Example)
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
	log.Println("Updating configuration directory permissions")
	err = system.Chown(configDirectory, true, "root", username)
	if err != nil {
		return fmt.Errorf("failed to change ownership of configuration: %w", err)
	}

	// Write service file
	log.Println("Writing service file")
	err = os.WriteFile(serviceFile, servicefile, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write relayer service: %w", err)
	}

	log.Println("Reload service daemon")
	err = svcHandler.DaemonReload()
	if err != nil {
		return fmt.Errorf("failed to reload daemon: %w", err)
	}

	log.Println("Enable service")
	err = svcHandler.Enable(serviceName)
	if err != nil {
		return fmt.Errorf("failed to enable servce: %w", err)
	}

	log.Println("Restart service")
	err = svcHandler.Restart(serviceName)
	if err != nil {
		return fmt.Errorf("failed to restart servce: %w", err)
	}
	return nil
}

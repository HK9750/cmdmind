package safety

import "strings"

var dangerousMarkers = []string{
	"rm -rf",
	"rm -fr",
	"sudo reboot",
	"sudo shutdown",
	"shutdown now",
	"docker system prune",
	"drop database",
	"truncate table",
	"mkfs",
	"dd if=",
	":(){:|:&};:",
	"> /dev/sd",
}

func IsDangerous(command string) bool {
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	for _, marker := range dangerousMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"maddog/internal/config"
	"maddog/internal/repair"
)

func repairCommand(args []string) int {
	guard := repair.NewStartupGuard(filepath.Join(config.MemoryUserDir(), "startup-probation.json"), 3, 2*time.Minute)
	if len(args) == 1 && args[0] == "reset-startup" {
		if err := guard.Reset(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Maddog startup recovery state reset")
		return 0
	}
	if len(args) == 0 || (len(args) == 1 && args[0] == "status") {
		b, _ := json.Marshal(guard.Status())
		fmt.Println(string(b))
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: maddog repair [status|reset-startup]")
	return 2
}

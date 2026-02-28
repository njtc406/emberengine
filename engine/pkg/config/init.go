package config

import (
	"fmt"
	"os"
)

//  包级辅助函数（无状态，保留）

const defaultConfPath = "./configs"
const startServiceConfName = "services.yaml"

func createDirIfNotExists(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return nil
}

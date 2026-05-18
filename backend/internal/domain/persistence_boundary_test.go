package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainDoesNotCarryDynamoDBTags(t *testing.T) {
	var offenders []string
	tag := "dynamo" + "dbav"
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), tag) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan domain: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("domain files must not contain DynamoDB row tags: %v", offenders)
	}
}

package language

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed reviewable_file_types.json
var reviewableFileTypesData []byte

var (
	reviewableOnce       sync.Once
	reviewableExtensions map[string]bool
	reviewableList       []string
)

func initReviewableExtensions() {
	if err := json.Unmarshal(reviewableFileTypesData, &reviewableList); err != nil {
		panic("language: failed to parse reviewable_file_types.json: " + err.Error())
	}
	reviewableExtensions = make(map[string]bool, len(reviewableList))
	for i, extension := range reviewableList {
		extension = strings.ToLower(extension)
		reviewableList[i] = extension
		reviewableExtensions[extension] = true
	}
	sort.Strings(reviewableList)
}

// IsReviewableExtension reports whether ccr reviews files with extension.
// The language boundary owns this set because both file admission and parser
// discovery must agree on which source families ccr accepts.
func IsReviewableExtension(extension string) bool {
	reviewableOnce.Do(initReviewableExtensions)
	return reviewableExtensions[strings.ToLower(extension)]
}

// ReviewableExtensions returns a sorted copy of ccr's admitted file suffixes.
func ReviewableExtensions() []string {
	reviewableOnce.Do(initReviewableExtensions)
	return append([]string(nil), reviewableList...)
}

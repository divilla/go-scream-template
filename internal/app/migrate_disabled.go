//go:build !migrate

package app

// Migrate is disabled in builds without the migrate tag.
func Migrate() {}

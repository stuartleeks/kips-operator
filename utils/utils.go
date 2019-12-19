package utils

import (
	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

// IgnoreNotFound returns nil for a NotFound error or the original error
func IgnoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// IgnoreConflict returns nil for a Conflict error or the original error
func IgnoreConflict(err error) error {
	if apierrs.IsConflict(err) {
		return nil
	}
	return err
}

// ContainsString returns true if the slice contains the specific string
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// RemoveString returns a new slice with the specifed string removed
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

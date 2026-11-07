//go:build !linux

package engine

import "context"

func HasCaps(string) (bool, error)            { return true, nil }
func GrantCaps(context.Context, string) error { return nil }
func needsCaps(string) (bool, error)          { return false, nil }

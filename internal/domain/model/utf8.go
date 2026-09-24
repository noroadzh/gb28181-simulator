// Package model — utf8 helper.
package model

import "unicode/utf8"

// utf8Valid is a thin wrapper so the public API in credentials.go stays
// readable; behaviour is identical to utf8.ValidString.
func utf8Valid(s string) bool { return utf8.ValidString(s) }
package main

import "github.com/emvi/iso-639-1"

type LanguageKey string

func (c LanguageKey) IsValid() bool {
	return iso6391.ValidCode(string(c))
}

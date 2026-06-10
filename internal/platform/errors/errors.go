package errors

import (
	"errors"
	"fmt"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindNotFound
	KindConflict
	KindValidation
	KindUnauthorized
	KindProvider
)

type Error struct {
	Kind Kind
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func New(kind Kind, msg string) *Error             { return &Error{Kind: kind, Msg: msg} }
func Wrap(kind Kind, msg string, err error) *Error { return &Error{Kind: kind, Msg: msg, Err: err} }

func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

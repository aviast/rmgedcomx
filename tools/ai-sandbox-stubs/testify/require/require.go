package require

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type TestingT interface {
	Errorf(format string, args ...interface{})
	FailNow()
}

func fail(t TestingT, msg string, msgAndArgs ...interface{}) {
	extra := ""
	if len(msgAndArgs) > 0 {
		if s, ok := msgAndArgs[0].(string); ok {
			extra = "\n" + fmt.Sprintf(s, msgAndArgs[1:]...)
		}
	}
	t.Errorf("%s%s", msg, extra)
	t.FailNow()
}

func NoError(t TestingT, err error, msgAndArgs ...interface{}) {
	if err != nil {
		fail(t, fmt.Sprintf("unexpected error: %v", err), msgAndArgs...)
	}
}

func Equal(t TestingT, expected, actual interface{}, msgAndArgs ...interface{}) {
	if !reflect.DeepEqual(expected, actual) {
		fail(t, fmt.Sprintf("Not equal:\nexpected: %#v\nactual  : %#v", expected, actual), msgAndArgs...)
	}
}

func NotEqual(t TestingT, expected, actual interface{}, msgAndArgs ...interface{}) {
	if reflect.DeepEqual(expected, actual) {
		fail(t, fmt.Sprintf("Should not be equal: %#v", actual), msgAndArgs...)
	}
}

func Empty(t TestingT, s string, msgAndArgs ...interface{}) {
	if s != "" {
		fail(t, fmt.Sprintf("expected empty, got %q", s), msgAndArgs...)
	}
}

func Error(t TestingT, err error, msgAndArgs ...interface{}) {
	if err == nil {
		fail(t, "expected an error, got nil", msgAndArgs...)
	}
}

func Contains(t TestingT, s, contains string, msgAndArgs ...interface{}) {
	if !strings.Contains(s, contains) {
		fail(t, fmt.Sprintf("%q does not contain %q", s, contains), msgAndArgs...)
	}
}

func NotContains(t TestingT, s, contains string, msgAndArgs ...interface{}) {
	if strings.Contains(s, contains) {
		fail(t, fmt.Sprintf("%q should not contain %q", s, contains), msgAndArgs...)
	}
}

func JSONEq(t TestingT, expected, actual string, msgAndArgs ...interface{}) {
	var e, a interface{}
	if err := json.Unmarshal([]byte(expected), &e); err != nil {
		fail(t, fmt.Sprintf("Expected value is not valid JSON: %v", err), msgAndArgs...)
		return
	}
	if err := json.Unmarshal([]byte(actual), &a); err != nil {
		fail(t, fmt.Sprintf("Actual value is not valid JSON: %v", err), msgAndArgs...)
		return
	}
	if !reflect.DeepEqual(e, a) {
		fail(t, fmt.Sprintf("Not equal JSON:\nexpected: %s\nactual  : %s", expected, actual), msgAndArgs...)
	}
}

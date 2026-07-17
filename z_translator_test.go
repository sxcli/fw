// Copyright 2026 Plamen K. Kosseff
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sxclifw

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type trCfg struct {
	Locale string `json:"locale" arg:"locale" usage:"locale override"`
}

type fakeTranslator struct {
	cfg   trCfg
	log   *[]string
	fail  bool
	table map[string]string
}

func (f *fakeTranslator) Configured() error {
	*f.log = append(*f.log, "translator.configured")
	var err error
	if f.fail {
		err = errors.New("bad catalog")
	}
	return err
}

func (f *fakeTranslator) Translate(msgid string) (string, bool) {
	s, ok := f.table[msgid]
	return s, ok
}

func (f *fakeTranslator) TranslateN(msgid, msgidPlural string, n int) (string, bool) {
	s, ok := f.table[fmt.Sprintf("%s|%s|%d", msgid, msgidPlural, n)]
	return s, ok
}

// a second concrete type, for the multiplicity violation
type secondTranslator struct{ fakeTranslator }

func translatorWorld(t *testing.T, argv []string, fail bool, table map[string]string) (*world, *fakeTranslator) {
	t.Helper()
	w := newWorld(t, argv, nil, nil)
	w.applet(0)
	f := &fakeTranslator{log: &w.log, fail: fail, table: table}
	w.rt.reg.Register("i18n", f, foldOptions([]RegisterOption{
		Provides[Translator](),
		WithConfig(&f.cfg),
	}))
	return w, f
}

func TestTranslatorConfiguredFirstAndActive(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin"}, false, map[string]string{"hello": "здравей"})
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "translator.configured,applet.configured,applet.run" {
		t.Errorf("translator must be configured first: %v", w.log)
	}
	if Tr("hello") != "здравей" {
		t.Errorf("translator not active: %q", Tr("hello"))
	}
}

func TestHelpRendersTranslated(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin", "--help"}, false, map[string]string{
		"the greeting": "поздравът",
	})
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stdout.String(), "поздравът") {
		t.Errorf("help must render translated usage:\n%s", w.stdout.String())
	}
	if !strings.Contains(strings.Join(w.log, ","), "translator.configured") {
		t.Errorf("help short-circuit must configure the translator: %v", w.log)
	}
}

func TestWriteConfigConfiguresTranslator(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin", "--write-config"}, false, nil)
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if !strings.Contains(strings.Join(w.log, ","), "translator.configured") {
		t.Errorf("write-config short-circuit must configure the translator: %v", w.log)
	}
}

func TestTwoTranslatorsAreAViolation(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin"}, false, nil)
	second := &secondTranslator{}
	second.log = &w.log
	w.rt.reg.Register("other", second, foldOptions([]RegisterOption{Provides[Translator]()}))
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "both provide Translator") {
		t.Errorf("violation not reported:\n%s", w.stderr.String())
	}
}

func TestTranslatorFailureDegradesQuietly(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin"}, true, map[string]string{"hello": "здравей"})
	if code := run(w.rt); code != 0 {
		t.Fatalf("translator failure must not fail the run: exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if Tr("hello") != "hello" {
		t.Errorf("degraded translator must not translate: %q", Tr("hello"))
	}
	if !strings.Contains(w.stderr.String(), "untranslated") {
		t.Errorf("degradation warning missing:\n%s", w.stderr.String())
	}
	if strings.Join(w.log, ",") != "translator.configured,applet.configured,applet.run" {
		t.Errorf("applet must still run, translator not re-configured: %v", w.log)
	}
}

func TestDisabledTranslatorMeansMsgids(t *testing.T) {
	w, _ := translatorWorld(t, []string{"bin", "--disable", "i18n"}, false, map[string]string{"hello": "здравей"})
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Contains(strings.Join(w.log, ","), "translator.configured") {
		t.Errorf("disabled translator must not be configured: %v", w.log)
	}
	if Tr("hello") != "hello" {
		t.Errorf("disabled translator must not translate: %q", Tr("hello"))
	}
}

func TestTrNEnglishFallbackAndImplicitN(t *testing.T) {
	if got := TrN("{n} window closed", "{n} windows closed", 1); got != "1 window closed" {
		t.Errorf("singular fallback wrong: %q", got)
	}
	if got := TrN("{n} window closed", "{n} windows closed", 5); got != "5 windows closed" {
		t.Errorf("plural fallback wrong: %q", got)
	}
	// the implicit binding shadows a caller-supplied "n"
	if got := TrN("{n}", "{n} many", 2, "n", "X"); got != "2 many" {
		t.Errorf("implicit n must shadow the caller's: %q", got)
	}
}

func TestTrNUsesTranslatorForms(t *testing.T) {
	activeTranslator = &fakeTranslator{table: map[string]string{
		"{n} window|{n} windows|5": "{n} прозореца",
	}}
	defer func() { activeTranslator = nil }()
	if got := TrN("{n} window", "{n} windows", 5); got != "5 прозореца" {
		t.Errorf("count form lost: %q", got)
	}
	// miss → English fallback even with a translator present
	if got := TrN("{n} window", "{n} windows", 2); got != "2 windows" {
		t.Errorf("miss fallback wrong: %q", got)
	}
}

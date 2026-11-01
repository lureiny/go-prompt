package prompt

import (
	"context"
	"encoding"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A CommandOption configures a command registered with Prompt.Command or
// Prompt.RawCommand: Flag, Exit, Completer and OnResult.
type CommandOption interface {
	applyCommand(c *command) error
}

type commandOptionFunc func(c *command) error

func (f commandOptionFunc) applyCommand(c *command) error { return f(c) }

// FlagSpec names one plain (non-struct) parameter of a command function. Pass
// one Flag per parameter, in parameter order:
//
//	p.Command("calc", "add two numbers", func(a, b int) { ... },
//		prompt.Flag("a", "first number").Default(1),
//		prompt.Flag("b", "second number"))
//
// For many parameters, a struct parameter with tags is usually clearer; see
// Prompt.Command.
type FlagSpec struct {
	name, help string
	def        any
	hasDef     bool
}

// Flag describes a command parameter: it is set on the command line with
// -name value (or -name=value; a bool flag also as plain -name).
func Flag(name, help string) *FlagSpec { return &FlagSpec{name: name, help: help} }

// Default sets the value used when the flag is omitted. It may be a value of
// the parameter's type, a number losslessly convertible to it (Default(5) for
// an int64 or float32), or a string in command-line syntax (Default("1m30s")
// for a time.Duration).
func (f *FlagSpec) Default(v any) *FlagSpec {
	f.def, f.hasDef = v, true
	return f
}

func (f *FlagSpec) applyCommand(c *command) error {
	c.flagSpecs = append(c.flagSpecs, f)
	return nil
}

// Exit makes the program quit after the command runs successfully.
func Exit() CommandOption {
	return commandOptionFunc(func(c *command) error {
		c.exitAfter = true
		return nil
	})
}

// Completer replaces the default suggestions offered while typing the
// command's arguments. f receives the input line up to the cursor.
func Completer(f func(input string) []Suggestion) CommandOption {
	return commandOptionFunc(func(c *command) error {
		c.completer = f
		return nil
	})
}

// OnResult receives the command function's return values (excluding a final
// error, which is reported as a failure; when it is non-nil OnResult isn't
// called). It overrides the prompt-wide
// WithResultHandler for this command.
func OnResult(f func(results []any)) CommandOption {
	return commandOptionFunc(func(c *command) error {
		c.onResult, c.hasOnResult = f, true
		return nil
	})
}

// Suggestion is one completion candidate.
type Suggestion struct {
	Text        string
	Description string
}

// command is a registered command: its function, how its arguments are
// bound, and its options.
type command struct {
	name, help string
	raw        bool // RawCommand: the function receives the argument string
	fn         reflect.Value
	takesCtx   bool

	// flag binding: either one struct parameter (argsType != nil) whose
	// fields are the flags, or one flag per plain parameter
	argsType  reflect.Type
	argsPtr   bool // the struct parameter is a pointer
	flags     []*flagDef
	flagSpecs []*FlagSpec

	exitAfter   bool
	completer   func(input string) []Suggestion
	onResult    func(results []any)
	hasOnResult bool
}

// flagDef is one bound flag.
type flagDef struct {
	name, help string
	typ        reflect.Type
	def        reflect.Value // default value, of typ
	isBool     bool
	field      []int // struct field index (struct binding)
}

var (
	errorType     = reflect.TypeOf((*error)(nil)).Elem()
	contextType   = reflect.TypeOf((*context.Context)(nil)).Elem()
	flagValueType = reflect.TypeOf((*flag.Value)(nil)).Elem()
	textUnmarshal = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	durationType  = reflect.TypeOf(time.Duration(0))
)

// newCommand validates fn and builds a command. raw selects RawCommand
// semantics.
func newCommand(name, help string, fn any, raw bool, opts []CommandOption) (*command, error) {
	if name == "" {
		return nil, errors.New("command name can't be empty")
	}
	if strings.ContainsFunc(name, unicode.IsSpace) {
		return nil, fmt.Errorf("command name %q can't contain whitespace", name)
	}
	if strings.HasPrefix(name, "-") {
		return nil, fmt.Errorf("command name %q can't start with '-'", name)
	}
	c := &command{name: name, help: help, raw: raw}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt.applyCommand(c); err != nil {
			return nil, fmt.Errorf("command[%s]: %w", name, err)
		}
	}
	if err := c.bind(fn); err != nil {
		return nil, fmt.Errorf("command[%s]: %w", name, err)
	}
	return c, nil
}

// bind checks fn's signature and prepares the flag definitions.
func (c *command) bind(fn any) error {
	if fn == nil {
		return errors.New("function is nil")
	}
	v := reflect.ValueOf(fn)
	t := v.Type()
	if t.Kind() != reflect.Func {
		return fmt.Errorf("want a function, got %s", t)
	}
	if v.IsNil() {
		return errors.New("function is nil")
	}
	if t.IsVariadic() {
		return errors.New("variadic functions are not supported")
	}
	c.fn = v
	in := make([]reflect.Type, 0, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		in = append(in, t.In(i))
	}
	if len(in) > 0 && in[0] == contextType {
		c.takesCtx = true
		in = in[1:]
	}

	if c.raw {
		if len(c.flagSpecs) > 0 {
			return errors.New("RawCommand takes no Flag options")
		}
		if len(in) != 1 || in[0].Kind() != reflect.String {
			return fmt.Errorf("RawCommand function must be func(string) or func(context.Context, string), got %s", t)
		}
		return nil
	}

	// one struct parameter (that isn't itself a bindable value type such as
	// time.Time) and no Flag options: the struct fields are the flags
	if len(in) == 1 && len(c.flagSpecs) == 0 && !bindable(in[0]) {
		st := in[0]
		if st.Kind() == reflect.Pointer && st.Elem().Kind() == reflect.Struct && !bindable(st.Elem()) {
			c.argsPtr, st = true, st.Elem()
		}
		if st.Kind() == reflect.Struct {
			c.argsType = st
			return c.bindStruct(st)
		}
	}

	if len(c.flagSpecs) != len(in) {
		names := make([]string, len(in))
		for i, p := range in {
			names[i] = p.String()
		}
		return fmt.Errorf("function has %d parameter(s) (%s) but %d Flag option(s); "+
			"pass one prompt.Flag per parameter, or use a single struct parameter",
			len(in), strings.Join(names, ", "), len(c.flagSpecs))
	}
	for i, spec := range c.flagSpecs {
		fd, err := newFlagDef(spec.name, spec.help, in[i])
		if err != nil {
			return err
		}
		if spec.hasDef {
			if fd.def, err = defaultValue(spec.def, in[i]); err != nil {
				return fmt.Errorf("flag -%s: %w", spec.name, err)
			}
		}
		c.flags = append(c.flags, fd)
	}
	return c.checkFlagNames()
}

// bindStruct turns the exported fields of st into flags. Tags:
//
//	flag:"name"      flag name (default: the field name in kebab-case)
//	flag:"-"         skip the field
//	help:"text"      description
//	default:"value"  default, in command-line syntax
func (c *command) bindStruct(st reflect.Type) error {
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Tag.Get("flag")
		if name == "-" {
			continue
		}
		if name == "" {
			name = kebabCase(f.Name)
		}
		fd, err := newFlagDef(name, f.Tag.Get("help"), f.Type)
		if err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		fd.field = f.Index
		if d, ok := f.Tag.Lookup("default"); ok {
			if err := setFromString(fd.def, d); err != nil {
				return fmt.Errorf("field %s: bad default %q: %w", f.Name, d, err)
			}
		}
		c.flags = append(c.flags, fd)
	}
	return c.checkFlagNames()
}

func (c *command) checkFlagNames() error {
	seen := map[string]bool{}
	for _, fd := range c.flags {
		if fd.name == "" || strings.HasPrefix(fd.name, "-") ||
			strings.ContainsAny(fd.name, "=") || strings.ContainsFunc(fd.name, unicode.IsSpace) {
			return fmt.Errorf("invalid flag name %q (must be non-empty, without a leading '-', '=' or spaces)", fd.name)
		}
		if seen[fd.name] {
			return fmt.Errorf("duplicate flag -%s", fd.name)
		}
		seen[fd.name] = true
	}
	return nil
}

func newFlagDef(name, help string, t reflect.Type) (*flagDef, error) {
	if !bindable(t) {
		return nil, fmt.Errorf("flag -%s: unsupported type %s (use a string, bool, number, "+
			"time.Duration, or a type implementing flag.Value or encoding.TextUnmarshaler)", name, t)
	}
	fd := &flagDef{name: name, help: help, typ: t, def: reflect.New(t).Elem()}
	fd.isBool = t.Kind() == reflect.Bool && !implementsSetter(t)
	if bf, ok := reflect.New(t).Interface().(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
		fd.isBool = true
	}
	return fd, nil
}

// bindable reports whether a parameter of type t can be set from one
// command-line word.
func bindable(t reflect.Type) bool {
	if implementsSetter(t) || t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// implementsSetter: *t is a flag.Value or an encoding.TextUnmarshaler.
func implementsSetter(t reflect.Type) bool {
	p := reflect.PointerTo(t)
	return p.Implements(flagValueType) || p.Implements(textUnmarshal)
}

// setFromString parses s into v (addressable, of a bindable type).
func setFromString(v reflect.Value, s string) error {
	p := v.Addr().Interface()
	if fv, ok := p.(flag.Value); ok {
		return fv.Set(s)
	}
	if tu, ok := p.(encoding.TextUnmarshaler); ok {
		return tu.UnmarshalText([]byte(s))
	}
	if v.Type() == durationType {
		d, err := time.ParseDuration(s)
		if err != nil {
			return errors.New("invalid duration (e.g. 1m30s)")
		}
		v.SetInt(int64(d))
		return nil
	}
	bits := 0
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		bits = v.Type().Bits()
	}
	var err error
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		var b bool
		if b, err = strconv.ParseBool(s); err == nil {
			v.SetBool(b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		if n, err = strconv.ParseInt(s, 0, bits); err == nil {
			v.SetInt(n)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var n uint64
		if n, err = strconv.ParseUint(s, 0, bits); err == nil {
			v.SetUint(n)
		}
	case reflect.Float32, reflect.Float64:
		var f float64
		if f, err = strconv.ParseFloat(s, bits); err == nil {
			v.SetFloat(f)
		}
	default:
		return fmt.Errorf("unsupported type %s", v.Type())
	}
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		if errors.Is(ne.Err, strconv.ErrRange) {
			return fmt.Errorf("out of range for %s", v.Type())
		}
		return fmt.Errorf("not a valid %s", v.Type())
	}
	return err
}

// defaultValue converts a Flag(...).Default value to t: the same type, a
// losslessly convertible number, or a string in command-line syntax.
func defaultValue(def any, t reflect.Type) (reflect.Value, error) {
	out := reflect.New(t).Elem()
	if def == nil {
		return out, nil
	}
	dv := reflect.ValueOf(def)
	switch {
	case dv.Type() == t:
		out.Set(dv)
	case dv.Kind() == reflect.String && t.Kind() != reflect.String:
		if err := setFromString(out, dv.String()); err != nil {
			return out, fmt.Errorf("default %q: %w", def, err)
		}
	case t != durationType && !implementsSetter(t):
		c, ok := convertNumber(dv, t)
		if !ok {
			return out, fmt.Errorf("default %v (%T) can't be used for a %s parameter", def, def, t)
		}
		out.Set(c)
	default:
		return out, fmt.Errorf("default %v (%T) can't be used for a %s parameter", def, def, t)
	}
	return out, nil
}

// convertNumber converts v to t when both are numeric and the conversion is
// lossless (the value survives a round trip): it rejects overflow (300 ->
// int8), truncation (1.5 -> int) and sign loss (-1 -> uint). Between floats
// only overflow is rejected, since float32 can't hold most float64 values
// exactly (0.1).
func convertNumber(v reflect.Value, t reflect.Type) (reflect.Value, bool) {
	if !isNumeric(v.Kind()) || !isNumeric(t.Kind()) {
		return reflect.Value{}, false
	}
	c := v.Convert(t)
	if v.CanFloat() && c.CanFloat() {
		return c, math.IsInf(v.Float(), 0) || !math.IsInf(c.Float(), 0)
	}
	if c.Convert(v.Type()).Interface() != v.Interface() {
		return reflect.Value{}, false
	}
	if v.CanInt() && v.Int() < 0 && c.CanUint() {
		return reflect.Value{}, false
	}
	return c, true
}

func isNumeric(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// kebabCase converts a Go field name to a flag name: MaxCount -> max-count,
// UserID -> user-id, A -> a. Non-ASCII names are lowercased as they are.
func kebabCase(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) {
			prevLower := i > 0 && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]))
			nextLower := i > 0 && i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1])
			if prevLower || nextLower {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(c))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// flagValue adapts an addressable reflect.Value to flag.Value.
type flagValue struct {
	v      reflect.Value
	isBool bool
}

func (f *flagValue) String() string {
	if !f.v.IsValid() {
		return ""
	}
	return fmt.Sprint(f.v.Interface())
}
func (f *flagValue) Set(s string) error { return setFromString(f.v, s) }
func (f *flagValue) IsBoolFlag() bool   { return f.isBool }

// usageError is returned by run when the user asked for help (-h, -help,
// --help); its text is the command's usage and is printed as-is.
type usageError struct{ usage string }

func (e *usageError) Error() string { return e.usage }

// run parses line (the full command line, including the command name) and
// calls the function. defaultOnResult receives the return values unless the
// command has its own OnResult.
func (c *command) run(ctx context.Context, line string, defaultOnResult func([]any)) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	var args []reflect.Value
	if c.takesCtx {
		args = append(args, reflect.ValueOf(&ctx).Elem())
	}
	if c.raw {
		arg := ""
		if parts := strings.SplitN(strings.TrimSpace(line), " ", 2); len(parts) == 2 {
			arg = strings.TrimSpace(parts[1])
		}
		args = append(args, reflect.ValueOf(arg))
	} else {
		bound, err := c.parseArgs(line)
		if err != nil {
			return err
		}
		args = append(args, bound...)
	}

	results := c.fn.Call(args)
	if n := len(results); n > 0 && c.fn.Type().Out(n-1) == errorType {
		if !results[n-1].IsNil() {
			err = results[n-1].Interface().(error)
		}
		results = results[:n-1]
	}
	onResult := defaultOnResult
	if c.hasOnResult {
		onResult = c.onResult
	}
	// on failure the other results are meaningless (e.g. "return 0, err")
	if err == nil && onResult != nil && len(results) > 0 {
		vals := make([]any, len(results))
		for i, r := range results {
			vals[i] = r.Interface()
		}
		onResult(vals)
	}
	return err
}

// parseArgs parses the words after the command name into argument values,
// starting from fresh defaults every time.
func (c *command) parseArgs(line string) ([]reflect.Value, error) {
	tokens := tokenize(line)
	if endsInOpenQuote(tokens) {
		return nil, errors.New("unterminated quote")
	}
	words := tokenTexts(tokens)
	if len(words) > 0 {
		words = words[1:] // the command name
	}

	fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are returned, not printed
	var structVal reflect.Value
	values := make([]reflect.Value, len(c.flags))
	if c.argsType != nil {
		structVal = reflect.New(c.argsType).Elem()
	}
	for i, fd := range c.flags {
		var v reflect.Value
		if c.argsType != nil {
			v = structVal.FieldByIndex(fd.field)
		} else {
			v = reflect.New(fd.typ).Elem()
		}
		v.Set(fd.def)
		values[i] = v
		fs.Var(&flagValue{v: v, isBool: fd.isBool}, fd.name, fd.help)
	}
	if err := fs.Parse(words); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, &usageError{usage: c.usage()}
		}
		return nil, err
	}
	// flag.Parse stops at the first non-flag word; don't drop the rest
	if rest := fs.Args(); len(rest) > 0 {
		return nil, fmt.Errorf("unexpected argument(s) %q (quote values that contain spaces, "+
			"e.g. -name \"a b\"; bool flags take -name=false)", rest)
	}
	if c.argsType != nil {
		if c.argsPtr {
			return []reflect.Value{structVal.Addr()}, nil
		}
		return []reflect.Value{structVal}, nil
	}
	return values, nil
}

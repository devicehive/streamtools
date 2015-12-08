package util

import (
	"fmt"
	"github.com/godbus/dbus"
	"reflect"
	"strings"
)

// D-Bus connection helper
type dbusConn struct {
	dbus       *dbus.Conn
	exclusive  bool
	matchRules map[string]bool
	Signals    chan *dbus.Signal
}

// newDBusConn() creates new D-Bus connection helper
func NewDBusConn() (conn *dbusConn) {
	conn = &dbusConn{}
	conn.matchRules = map[string]bool{}
	return
}

// isOpen() checks if D-Bus connection is open
func (conn *dbusConn) IsOpen() bool {
	return conn.dbus != nil
}

// open() tries to open D-Bus connection
func (conn *dbusConn) Open(address string) (err error) {
	switch strings.ToLower(address) {
	case "@system", "system":
		conn.dbus, err = dbus.SystemBus()
		conn.exclusive = false

	case "@session", "session":
		conn.dbus, err = dbus.SessionBus()
		conn.exclusive = false

	default: // dial by address
		conn.dbus, err = dbus.Dial(address)
		conn.exclusive = true
		if err != nil {
			return
		}

		// authenticate
		err = conn.dbus.Auth(nil)
		if err != nil {
			conn.dbus.Close()
			conn.dbus = nil
			return
		}

		// initialize
		err = conn.dbus.Hello()
		if err != nil {
			conn.dbus.Close()
			conn.dbus = nil
			return
		}
	}

	return
}

// close() closes the D-Bus connection
func (conn *dbusConn) Close() (err error) {
	if conn.dbus != nil {
		// TODO: delete signals from D-Bus connection!?
		if conn.exclusive {
			err = conn.dbus.Close()
		}
		conn.dbus = nil
	}

	return
}

// Object() returns the D-Bus object
func (conn *dbusConn) Object(dest, path string) dbus.BusObject {
	return conn.dbus.Object(dest, dbus.ObjectPath(path))
}

// watchSignals() starts signal watching on open D-Bus connection
func (conn *dbusConn) WatchSignals() (err error) {
	// create channel
	if conn.Signals == nil {
		conn.Signals = make(chan *dbus.Signal, 1024)
	}

	// watch for signals via channel
	if conn.dbus != nil {
		conn.dbus.Signal(conn.Signals)
	}

	return
}

// insertMatchRule() adds match rule to the D-Bus object
func (conn *dbusConn) InsertMatchRule(rule string) (err error) {
	// duplicates are not allowed
	if conn.dbus != nil && !conn.matchRules[rule] {
		call := conn.dbus.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule)
		err = call.Err

		// add rule to the matchRules set
		if err == nil {
			conn.matchRules[rule] = true
		}
	}

	return
}

// removeMatchRule() removes match rule from the D-Bus object
func (conn *dbusConn) RemoveMatchRule(rule string) (err error) {
	if conn.dbus != nil && conn.matchRules[rule] {
		call := conn.dbus.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
		err = call.Err

		// remove rule from the matchRules set
		if err == nil {
			delete(conn.matchRules, rule)
		}
	}

	return
}

// removeAllMatchRules() removes all installed match rules from the D-Bus object
func (conn *dbusConn) RemoveAllMatchRules(force bool) (err error) {
	for rule, _ := range conn.matchRules {
		err = conn.RemoveMatchRule(rule)
		if err != nil && !force {
			break
		}
	}

	// clear all rules anyway
	if force {
		conn.matchRules = map[string]bool{}
	}

	return
}

// convert value to another type
func convTo(arg interface{}, to reflect.Type) (res interface{}, err error) {
	val := reflect.ValueOf(arg)
	if val.Type().ConvertibleTo(to) {
		res = val.Convert(to).Interface()
	} else {
		err = fmt.Errorf("%T:%v is not convertible to %v", arg, arg, to)
	}

	return
}

// find matching pattern
func match(s string, left, right rune) int {
	n := 0 // match depth
	for i, v := range s {
		if v == left {
			n++
		} else if v == right {
			n--
			if n == 0 {
				return i
			}
		}
	}
	return -1 // not found
}

// get corresponding type according to provided D-Bus signature
func dbusTypeFor(sig string) (t reflect.Type, rem string, err error) {
	if len(sig) == 0 {
		err = fmt.Errorf("%q - bad signature: empty", sig)
		return
	}

	switch sig[0] {
	case 'y':
		t = reflect.TypeOf(byte(0))
		rem = sig[1:]
	case 'b':
		t = reflect.TypeOf(false)
		rem = sig[1:]
	case 'n':
		t = reflect.TypeOf(int16(0))
		rem = sig[1:]
	case 'q':
		t = reflect.TypeOf(uint16(0))
		rem = sig[1:]
	case 'i':
		t = reflect.TypeOf(int32(0))
		rem = sig[1:]
	case 'u':
		t = reflect.TypeOf(uint32(0))
		rem = sig[1:]
	case 'x':
		t = reflect.TypeOf(int64(0))
		rem = sig[1:]
	case 't':
		t = reflect.TypeOf(uint64(0))
		rem = sig[1:]
	case 'd':
		t = reflect.TypeOf(float64(0.0))
		rem = sig[1:]
	case 's':
		t = reflect.TypeOf("")
		rem = sig[1:]
	case 'g':
		t = reflect.TypeOf(dbus.Signature{})
		rem = sig[1:]
	case 'o':
		t = reflect.TypeOf(dbus.ObjectPath(""))
		rem = sig[1:]
	case 'v':
		t = reflect.TypeOf(dbus.Variant{})
		rem = sig[1:]
	case 'h':
		t = reflect.TypeOf(dbus.UnixFD(0))
		rem = sig[1:]

	// array or map
	case 'a':
		if len(sig) > 1 && sig[1] == '{' { // map
			if i := match(sig, '{', '}'); i > 0 {
				rem = sig[i+1:]
				sig = sig[2:i] // omit a{ }
				if len(sig) <= 1 {
					err = fmt.Errorf("%q - bad signature: too short for dicts", sig)
					return
				}

				ksig := sig[0:1] // key signature
				vsig := sig[1:]  // value signature

				// key type
				kt, _, e := dbusTypeFor(ksig)
				if e != nil {
					err = fmt.Errorf("%q - bad dict's key signature: %s", ksig, e)
					return
				}
				// TODO: check key type is valid key type: integer or string

				// value type
				vt, _, e := dbusTypeFor(vsig)
				if e != nil {
					err = fmt.Errorf("%q - bad dict's value signature: %s", vsig, e)
					return
				}

				t = reflect.MapOf(kt, vt)
			} else {
				err = fmt.Errorf("%q - bad signature: unmatched '}'", sig)
			}
		} else { // array
			t, rem, err = dbusTypeFor(sig[1:])
			if err == nil {
				t = reflect.SliceOf(t)
			} else {
				t = nil
			}
		}

	// structure
	case '(':
		if i := match(sig, '(', ')'); i > 0 {
			rem = sig[i+1:]
			sig = sig[1:i] // omit ( )
			if len(sig) != 0 {
				// extract fields
				var types []reflect.Type
				for len(sig) != 0 {
					t, sig, err = dbusTypeFor(sig)
					if err != nil {
						t = nil
						return
					}
					types = append(types, t)
				}

				// there is no way in Go to build type dynamically
				// so return corresponding anonymous structure
				switch len(types) {
				// case 0: // impossible
				case 1:
					s := struct {
						a interface{}
					}{}
					t = reflect.TypeOf(s)
				case 2:
					s := struct {
						a, b interface{}
					}{}
					t = reflect.TypeOf(s)
				case 3:
					s := struct {
						a, b, c interface{}
					}{}
					t = reflect.TypeOf(s)
				case 4:
					s := struct {
						a, b, c, d interface{}
					}{}
					t = reflect.TypeOf(s)
				case 5:
					s := struct {
						a, b, c, d, e interface{}
					}{}
					t = reflect.TypeOf(s)
				case 6:
					s := struct {
						a, b, c, d, e, f interface{}
					}{}
					t = reflect.TypeOf(s)
				case 7:
					s := struct {
						a, b, c, d, e, f, g interface{}
					}{}
					t = reflect.TypeOf(s)
				case 8:
					s := struct {
						a, b, c, d, e, f, g, h interface{}
					}{}
					t = reflect.TypeOf(s)
				case 9:
					s := struct {
						a, b, c, d, e, f, g, h, i interface{}
					}{}
					t = reflect.TypeOf(s)
				case 10:
					s := struct {
						a, b, c, d, e, f, g, h, i, j interface{}
					}{}
					t = reflect.TypeOf(s)
				case 11:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k interface{}
					}{}
					t = reflect.TypeOf(s)
				case 12:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k, l interface{}
					}{}
					t = reflect.TypeOf(s)
				case 13:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k, l, m interface{}
					}{}
					t = reflect.TypeOf(s)
				case 14:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k, l, m, n interface{}
					}{}
					t = reflect.TypeOf(s)
				case 15:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k, l, m, n, o interface{}
					}{}
					t = reflect.TypeOf(s)
				case 16:
					s := struct {
						a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p interface{}
					}{}
					t = reflect.TypeOf(s)
				// TODO: more fields!?
				default:
					err = fmt.Errorf("%q - bad signature: structure has too many fields", sig)
				}
			} else {
				err = fmt.Errorf("%q - bad signature: empty structure", sig)
			}
		} else {
			err = fmt.Errorf("%q - bad signature: unmatched ')'", sig)
		}

	default:
		err = fmt.Errorf("%q - bad signature: unknown type character", sig)
	}

	return
}

// convert value according to D-Bus signature
func dbusConv(sig string, depth int, arg interface{}) (res interface{}, rem string, err error) {
	if len(sig) == 0 {
		err = fmt.Errorf("%q - bad signature: empty", sig)
		return
	}
	if depth > 64 {
		err = fmt.Errorf("%q - bad signature: nesting too deep", sig)
		return
	}

	switch sig[0] {
	// basic types
	case 'y', 'b', 'n', 'q', 'i', 'u', 'x', 't', 'd', 's', 'g', 'o', 'h':
		var to reflect.Type
		to, rem, err = dbusTypeFor(sig)
		if err == nil {
			res, err = convTo(arg, to)
			// TODO: one more chance? try to parse string?
		}

	// variant
	case 'v':
		rem = sig[1:]
		res = dbus.MakeVariant(arg)

	// array or map
	case 'a':
		if len(sig) > 1 && sig[1] == '{' { // map
			var toT reflect.Type // map type
			toT, rem, err = dbusTypeFor(sig)
			if err == nil {
				sig = sig[2 : len(sig)-len(rem)-1] // omit a{ }
				ksig := sig[0:1]                   // key signature
				vsig := sig[1:]                    // value signature

				fromV := reflect.ValueOf(arg)
				fromT := fromV.Type()

				// we can convert maps
				switch fromT.Kind() {
				case reflect.Map:
					toV := reflect.MakeMap(toT)
					for _, fromK := range fromV.MapKeys() {
						var k interface{}
						k, _, err = dbusConv(ksig, depth+1,
							fromK.Interface())
						if err != nil {
							return // break?
						}

						var v interface{}
						v, _, err = dbusConv(vsig, depth+1,
							fromV.MapIndex(fromK).Interface())
						if err != nil {
							return // break?
						}

						toV.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v))
					}
					res = toV.Interface() // done

				// TODO: convert nil to empty map!

				default:
					err = fmt.Errorf("unexpected data to convert map: %v", fromT)
				}
			}
		} else { // array
			var toT reflect.Type // array type
			toT, rem, err = dbusTypeFor(sig)
			if err == nil {
				sig = sig[1 : len(sig)-len(rem)] // element signature

				fromV := reflect.ValueOf(arg)
				fromT := fromV.Type()

				// we can convert slices
				switch fromT.Kind() {
				case reflect.Slice:
					toV := reflect.MakeSlice(toT, fromV.Len(), fromV.Cap())
					for i := 0; i < fromV.Len(); i++ {
						var v interface{}
						v, _, err = dbusConv(sig, depth+1,
							fromV.Index(i).Interface())
						if err != nil {
							return // break?
						}
						toV.Index(i).Set(reflect.ValueOf(v))
					}
					res = toV.Interface() // done

				// TODO: convert nil to empty array!
				// TODO: convert scalar to single-element array?

				default:
					err = fmt.Errorf("unexpected data to convert array: %v", fromT)
				}
			}
		}

	// structure
	case '(':
		var toT reflect.Type // structure type
		toT, rem, err = dbusTypeFor(sig)
		if err == nil {
			sig = sig[1 : len(sig)-len(rem)] // omit ( )

			fromV := reflect.ValueOf(arg)
			fromT := fromV.Type()

			// we can convert structures and slices
			switch fromT.Kind() {
			case reflect.Struct:
				if fromT.NumField() == toT.NumField() {
					toV := reflect.New(toT)
					// convert each field...
					for i := 0; len(sig) != 0; i++ {
						var v interface{}
						v, sig, err = dbusConv(sig, depth+1,
							fromV.Field(i).Interface())
						if err != nil {
							return // break?
						}
						toV.Field(i).Set(reflect.ValueOf(v))
					}
					res = toV.Interface() // done
				} else {
					err = fmt.Errorf("unable to convert struct: field count mismatch %v!=%v (signature: %q)",
						fromT.NumField(), toT.NumField(), sig)
				}

			case reflect.Slice:
				if fromV.Len() == toT.NumField() {
					toV := reflect.New(toT)
					// convert each element to corresponding field...
					for i := 0; len(sig) != 0; i++ {
						var v interface{}
						v, sig, err = dbusConv(sig, depth+1,
							fromV.Index(i).Interface())
						if err != nil {
							return // break?
						}
						toV.Field(i).Set(reflect.ValueOf(v))
					}
					res = toV.Interface() // done
				} else {
					err = fmt.Errorf("unable to convert struct: field count mismatch %v!=%v (signature: %q)",
						fromV.Len(), toT.NumField(), sig)
				}

			default:
				err = fmt.Errorf("unexpected data to convert struct: %v", fromT)
			}
		}

	default:
		err = fmt.Errorf("%q - bad signature: unknown type character", sig)
	}

	return
}

// apply D-Bus signature: convert types according to provided signature
func DBusConv(signature dbus.Signature, args ...interface{}) (res []interface{}, err error) {
	s := signature.String()
	for i := 0; err == nil && len(s) != 0; i++ {
		var r interface{}
		r, s, err = dbusConv(s, 0, args[i])
		if err != nil {
			break
		}
		res = append(res, r)
	}

	return
}

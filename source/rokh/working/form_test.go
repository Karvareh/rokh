package working

import "reflect"

// formFieldNames reports the Form's fields, so a test can assert that no name
// for a not-yet-existing event ever appears among them.
func formFieldNames() []string {
	t := reflect.TypeOf(Form{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Name)
	}
	return out
}

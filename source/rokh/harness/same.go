package harness

// BindSame binds a harness, and treats the very same declaration arriving
// again as the binding already standing rather than as a second harness.
//
// A program that restarts, or two of its processes that each look before they
// act, declare once between them; nothing about the covenant changes, so
// nothing is refused. A different declaration in a space that is taken is
// refused exactly as Bind refuses it, and a nested space still cannot be
// taken. Sameness is read after both declarations are checked and put in
// their one order, so the order the verbs were typed in is not a difference.
//
//	— T11.10, T13.5
func (r *Register) BindSame(c Covenant, e Effects) (already bool, err error) {
	cov, err := c.Bind()
	if err != nil {
		return false, err
	}
	eff, err := e.Declare()
	if err != nil {
		return false, err
	}
	if held, found := r.by[cov.Namespace]; found && sameCovenant(held.Covenant, cov) && held.Effects == eff {
		return true, nil
	}
	return false, r.Bind(c, e)
}

func sameCovenant(a, b Covenant) bool {
	if a.Namespace != b.Namespace || a.Version != b.Version || a.Unknown != b.Unknown || len(a.Can) != len(b.Can) {
		return false
	}
	for i := range a.Can {
		if a.Can[i] != b.Can[i] {
			return false
		}
	}
	return true
}

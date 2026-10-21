package ui

// formTools are the CLIs the open form steps through.
func formTools(m *Model) []string {
	names, _ := m.form.Tools()
	return names
}

// setFormTool picks the form's CLI by index, leaving its choice rows be.
func setFormTool(m *Model, index int) { m.form.SetTools(formTools(m), index) }

// quickTools are the CLIs the open quick bar steps through.
func quickTools(m *Model) []string {
	names, _ := m.quick.Tools()
	return names
}

// setQuickTool picks the quick bar's CLI by index, leaving its choice be.
func setQuickTool(m *Model, index int) { m.quick.SetTools(quickTools(m), index) }

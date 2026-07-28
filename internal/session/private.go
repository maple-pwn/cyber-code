package session

// RestrictPrivateDirectory limits a directory to the current user using the
// platform implementation used by the session store.
func RestrictPrivateDirectory(path string) error {
	return restrictDirectory(path)
}

// RestrictPrivateFile limits a file to the current user using Unix modes or a
// protected Windows DACL.
func RestrictPrivateFile(path string) error {
	return restrictFile(path)
}

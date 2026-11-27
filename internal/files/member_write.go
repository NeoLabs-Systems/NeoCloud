package files

func readOnlyShare() error {
	return apiError{403, "This folder is shared with you read-only.", "SHARE_READ_ONLY"}
}

func shareBoundary() error {
	return apiError{409, "Items can’t be moved across the border of a shared folder.", "SHARE_BOUNDARY"}
}

func shareRootImmutable() error {
	return apiError{403, "The shared folder itself belongs to its owner.", "SHARE_ROOT_IMMUTABLE"}
}

func validateConflictMode(conflict string) error {
	if conflict == "" || conflict == "fail" || conflict == "rename" || conflict == "replace" {
		return nil
	}
	return apiError{400, "conflict must be fail, rename or replace.", "CONFLICT_MODE_INVALID"}
}

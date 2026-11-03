package services

// SetUpdateFreeSpaceForTest replaces the filesystem seam only on this instance.
func SetUpdateFreeSpaceForTest(s *UpdateService, space func(string) (uint64, error)) {
	s.freeSpace = space
}

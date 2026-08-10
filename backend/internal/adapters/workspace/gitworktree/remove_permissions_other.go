//go:build !unix

package gitworktree

func makeOwnerDirectoriesWritable(string) (bool, error) {
	return false, nil
}

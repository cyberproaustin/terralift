// Package enumkit holds the shared, cloud-neutral scaffolding for the Enumerate
// phase: token-based pagination and native-type classification. Each provider
// still calls its own cloud APIs; the repetitive plumbing lives here once so a
// new provider fills in the API call, not the paging loop.
package enumkit

// Paginate accumulates every page of a token-paged API. fetchPage runs one page
// given the current token ("" for the first page) and returns that page's items
// and the next token ("" when there are no more pages). It is the shared form of
// the hand-rolled NextToken / SkipToken loops each provider carried.
func Paginate[T any](fetchPage func(token string) (items []T, next string, err error)) ([]T, error) {
	var all []T
	token := ""
	for {
		items, next, err := fetchPage(token)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		token = next
	}
}

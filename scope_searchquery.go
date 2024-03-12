package gocb

// Search executes the search request on the server using a scope-level FTS index.
func (s *Scope) Search(indexName string, request SearchRequest, opts *SearchOptions) (*SearchResult, error) {
	if request.VectorSearch == nil && request.SearchQuery == nil {
		return nil, makeInvalidArgumentsError("the search request cannot be empty")
	}

	if opts == nil {
		opts = &SearchOptions{}
	}

	provider, err := s.getSearchProvider()
	if err != nil {
		return nil, &SearchError{
			InnerError: wrapError(err, "failed to get search provider"),
			Query:      request,
		}
	}
	return provider.Search(s, indexName, request, opts)
}

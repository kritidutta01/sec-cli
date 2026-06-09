package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kritidutta01/sec-cli/internal/cache"
	"github.com/kritidutta01/sec-cli/internal/edgar"
)

// cacheCmd is the parent for cache-management subcommands. The cache itself is
// transparent — fetching commands use it automatically unless --no-cache is set;
// these subcommands expose its location and let the user wipe it.
func cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clear the local EDGAR cache",
	}
	cmd.AddCommand(cachePathCmd())
	cmd.AddCommand(cacheClearCmd())
	return cmd
}

// cachePathCmd prints the cache file location. It does not open the database, so
// it works even before the first fetch creates the file.
func cachePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the cache file location",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := cache.DefaultPath()
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

// cacheClearCmd truncates both cache layers, leaving an empty but valid file.
// It always operates on the real cache, ignoring --no-cache.
func cacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Remove all cached EDGAR bytes and parsed documents",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := cache.DefaultPath()
			if err != nil {
				return err
			}
			c, err := cache.Open(path)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			if err := c.Clear(); err != nil {
				return err
			}
			fmt.Println("cache cleared:", path)
			return nil
		},
	}
}

// openCache returns the cache a fetching command should use: a no-op cache when
// the persistent --no-cache flag is set, otherwise the SQLite cache at the
// default location. The caller is responsible for Close.
func openCache(cmd *cobra.Command) (*cache.Cache, error) {
	if noCache, _ := cmd.Flags().GetBool("no-cache"); noCache {
		return cache.NoOp(), nil
	}
	path, err := cache.DefaultPath()
	if err != nil {
		return nil, err
	}
	return cache.Open(path)
}

// fetchContext is the shared preamble the debug commands (fetch/detect/facts/
// table) run before touching a filing's bytes: a client, the resolved filing,
// and a caching fetch wired around Client.Get and honoring --no-cache. The
// caller must defer close to release the cache.
type fetchContext struct {
	client *edgar.Client
	cik    int64
	filing edgar.Filing
	fetch  cache.FetchFunc
	close  func() error
}

// newFetchContext resolves ticker→CIK→filing and opens the cache, returning a
// caching fetch the command uses for every EDGAR byte fetch.
func newFetchContext(cmd *cobra.Command, ticker, form string, year int) (*fetchContext, error) {
	client, err := edgar.NewClient()
	if err != nil {
		return nil, err
	}
	ctx := cmd.Context()

	cik, err := client.LookupCIK(ctx, ticker)
	if err != nil {
		return nil, err
	}

	var filing edgar.Filing
	if year != 0 {
		filing, err = client.FilingForYear(ctx, cik, form, year)
	} else {
		filing, err = client.LatestFiling(ctx, cik, form)
	}
	if err != nil {
		return nil, err
	}

	c, err := openCache(cmd)
	if err != nil {
		return nil, err
	}
	return &fetchContext{
		client: client,
		cik:    cik,
		filing: filing,
		fetch:  c.Fetching(client.Get),
		close:  c.Close,
	}, nil
}

package cred

// EnvAPIKey is the environment variable that overrides stored credentials.
const EnvAPIKey = "VIBEXP_API_KEY"

// Source identifies where a resolved bearer token came from.
type Source string

const (
	SourceNone   Source = ""
	SourceEnv    Source = "env"
	SourceStored Source = "stored"
)

// Resolved is the effective credential for a context.
type Resolved struct {
	Bearer string
	Source Source
	// Type is the stored entry type when Source is "stored" ("api_key" or
	// "oauth"); empty for env.
	Type string
}

// Resolve computes the effective bearer token for a context using precedence
// env (VIBEXP_API_KEY) > stored. A nil Getenv is treated as empty. When nothing
// is available, Source is SourceNone and Bearer is empty.
func (s *Store) Resolve(contextName string, getenv func(string) string) (Resolved, error) {
	if getenv != nil {
		if key := getenv(EnvAPIKey); key != "" {
			return Resolved{Bearer: key, Source: SourceEnv}, nil
		}
	}
	entry, err := s.Get(contextName)
	if err != nil {
		return Resolved{}, err
	}
	if entry == nil {
		return Resolved{Source: SourceNone}, nil
	}
	switch entry.Type {
	case TypeOAuth:
		return Resolved{Bearer: entry.AccessToken, Source: SourceStored, Type: TypeOAuth}, nil
	default: // api_key
		return Resolved{Bearer: entry.APIKey, Source: SourceStored, Type: TypeAPIKey}, nil
	}
}

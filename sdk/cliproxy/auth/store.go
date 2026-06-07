package auth

import "context"

// TokenStorage defines the SDK-visible contract for persisting credential payloads.
type TokenStorage interface {
	SaveTokenToFile(authFilePath string) error
}

// MetadataSetter is implemented by token storages that can accept arbitrary
// metadata before serializing themselves to disk.
type MetadataSetter interface {
	SetMetadata(map[string]any)
}

// ApplyStorageMetadata injects metadata into a token storage when supported.
func ApplyStorageMetadata(storage TokenStorage, meta map[string]any) {
	if storage == nil {
		return
	}
	if setter, ok := storage.(MetadataSetter); ok {
		setter.SetMetadata(meta)
	}
}

// Store abstracts persistence of Auth state across restarts.
type Store interface {
	// List returns all auth records stored in the backend.
	List(ctx context.Context) ([]*Auth, error)
	// Save persists the provided auth record, replacing any existing one with same ID.
	Save(ctx context.Context, auth *Auth) (string, error)
	// Delete removes the auth record identified by id.
	Delete(ctx context.Context, id string) error
}

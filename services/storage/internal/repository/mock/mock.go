package mock

import (
	"context"

	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository"
)

// Repository is a fake repository.Repository.
type Repository struct {
	// Canned results, returned by the matching method.
	File      *repository.File   // GetByID
	Files     []repository.File  // GetByIDs
	ListFiles []*repository.File // List
	Total     int                // List
	Err       error              // returned by whichever method is called

	// Captured arguments, for assertions.
	CreatedFile *repository.File
	GetByIDArg  string
	GetByIDsArg []string
	ListLimit   int
	ListOffset  int
}

func (r *Repository) Create(ctx context.Context, file *repository.File) error {
	r.CreatedFile = file
	return r.Err
}

func (r *Repository) GetByID(ctx context.Context, id string) (*repository.File, error) {
	r.GetByIDArg = id
	if r.Err != nil {
		return nil, r.Err
	}
	return r.File, nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []string) ([]repository.File, error) {
	r.GetByIDsArg = ids
	if r.Err != nil {
		return nil, r.Err
	}
	return r.Files, nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]*repository.File, int, error) {
	r.ListLimit, r.ListOffset = limit, offset
	if r.Err != nil {
		return nil, 0, r.Err
	}
	return r.ListFiles, r.Total, nil
}

var _ repository.Repository = (*Repository)(nil)
package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/mocks"
	"go.uber.org/mock/gomock"
)

func TestFileService_Upload(t *testing.T) {
	tests := []struct {
		name      string
		input     client.UploadInput
		mockSetup func(*mocks.MockStorageClient)
		want      domain.File
		wantErr   bool
	}{
		{
			name:  "success",
			input: client.UploadInput{Name: "a.txt", Size: 100, Content: strings.NewReader("x")},
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().Store(gomock.Any(), gomock.Any()).Return(
					domain.File{ID: "1", Name: "a.txt"}, nil,
				)
			},
			want:    domain.File{ID: "1", Name: "a.txt"},
			wantErr: false,
		},
		{
			name:    "file too large",
			input:   client.UploadInput{Name: "big.txt", Size: 1025},
			wantErr: true,
		},
		{
			name:  "storage error",
			input: client.UploadInput{Name: "fail.txt", Size: 10},
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().Store(gomock.Any(), gomock.Any()).Return(
					domain.File{}, errors.New("storage down"),
				)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockClient := mocks.NewMockStorageClient(ctrl)
			svc := NewFileService(mockClient, 1024)

			if tt.mockSetup != nil {
				tt.mockSetup(mockClient)
			}

			got, err := svc.Upload(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Upload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFileService_Download(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		mockSetup func(*mocks.MockStorageClient)
		wantFile  domain.File
		wantErr   bool
	}{
		{
			name: "proxies to storage",
			id:   "abc",
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().Fetch(gomock.Any(), "abc").Return(
					io.NopCloser(strings.NewReader("data")),
					domain.File{ID: "abc"}, nil,
				)
			},
			wantFile: domain.File{ID: "abc"},
			wantErr:  false,
		},
		{
			name: "not found",
			id:   "missing",
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().Fetch(gomock.Any(), "missing").Return(
					nil, domain.File{}, errors.New("not found"),
				)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockClient := mocks.NewMockStorageClient(ctrl)
			svc := NewFileService(mockClient, 1024)

			tt.mockSetup(mockClient)

			rc, file, err := svc.Download(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Download() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			defer rc.Close()

			if file != tt.wantFile {
				t.Errorf("file = %+v, want %+v", file, tt.wantFile)
			}
		})
	}
}

func TestFileService_List(t *testing.T) {
	tests := []struct {
		name      string
		page      int
		pageSize  int
		mockSetup func(*mocks.MockStorageClient)
		wantFiles []domain.File
		wantTotal int
		wantErr   bool
	}{
		{
			name:     "proxies pagination",
			page:     2,
			pageSize: 50,
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().List(gomock.Any(), 2, 50).Return(
					[]domain.File{{ID: "1"}}, 100, nil,
				)
			},
			wantFiles: []domain.File{{ID: "1"}},
			wantTotal: 100,
		},
		{
			name:     "service error",
			page:     1,
			pageSize: 20,
			mockSetup: func(m *mocks.MockStorageClient) {
				m.EXPECT().List(gomock.Any(), 1, 20).Return(
					nil, 0, errors.New("db down"),
				)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockClient := mocks.NewMockStorageClient(ctrl)
			svc := NewFileService(mockClient, 1024)

			tt.mockSetup(mockClient)

			files, total, err := svc.List(context.Background(), tt.page, tt.pageSize)
			if (err != nil) != tt.wantErr {
				t.Fatalf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(files) != len(tt.wantFiles) {
				t.Errorf("files count = %d, want %d", len(files), len(tt.wantFiles))
			}
		})
	}
}

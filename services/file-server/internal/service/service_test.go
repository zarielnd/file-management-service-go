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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockStorageClient(ctrl)
	svc := NewFileService(mockClient, 1024) // max 1KB

	tests := []struct {
		name      string
		input     client.UploadInput
		mockSetup func()
		wantErr   bool
	}{
		{
			name:  "success",
			input: client.UploadInput{Name: "a.txt", Size: 100, Content: strings.NewReader("x")},
			mockSetup: func() {
				mockClient.EXPECT().Store(gomock.Any(), gomock.Any()).Return(
					domain.File{ID: "1", Name: "a.txt"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:    "file too large",
			input:   client.UploadInput{Name: "big.txt", Size: 1025},
			wantErr: true,
			// Không gọi mock vì fail ở validation
		},
		{
			name:  "storage error",
			input: client.UploadInput{Name: "fail.txt", Size: 10},
			mockSetup: func() {
				mockClient.EXPECT().Store(gomock.Any(), gomock.Any()).Return(
					domain.File{}, errors.New("storage down"),
				)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				tt.mockSetup()
			}

			got, err := svc.Upload(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Upload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.ID != "1" {
				t.Errorf("id = %s, want 1", got.ID)
			}
		})
	}
}

func TestFileService_Download(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockStorageClient(ctrl)
	svc := NewFileService(mockClient, 1024)

	t.Run("proxies to storage", func(t *testing.T) {
		mockClient.EXPECT().Fetch(gomock.Any(), "abc").Return(
			io.NopCloser(strings.NewReader("data")),
			domain.File{ID: "abc"}, nil,
		)

		rc, file, err := svc.Download(context.Background(), "abc")
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()

		if file.ID != "abc" {
			t.Errorf("id = %s", file.ID)
		}
	})
}

func TestFileService_List(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockStorageClient(ctrl)
	svc := NewFileService(mockClient, 1024)

	t.Run("proxies pagination", func(t *testing.T) {
		mockClient.EXPECT().List(gomock.Any(), 2, 50).Return(
			[]domain.File{{ID: "1"}}, 100, nil,
		)

		files, total, err := svc.List(context.Background(), 2, 50)
		if err != nil || total != 100 || len(files) != 1 {
			t.Fatalf("List() = %v, %d, %v", files, total, err)
		}
	})
}
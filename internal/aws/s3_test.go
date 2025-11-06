package aws

import (
	"testing"
)

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{
			name:       "valid S3 URI",
			uri:        "s3://my-bucket/path/to/file.json",
			wantBucket: "my-bucket",
			wantKey:    "path/to/file.json",
			wantErr:    false,
		},
		{
			name:       "valid S3 URI with nested path",
			uri:        "s3://bucket-name/deep/nested/path/file.json",
			wantBucket: "bucket-name",
			wantKey:    "deep/nested/path/file.json",
			wantErr:    false,
		},
		{
			name:       "valid S3 URI with single level",
			uri:        "s3://bucket/file.json",
			wantBucket: "bucket",
			wantKey:    "file.json",
			wantErr:    false,
		},
		{
			name:    "missing s3:// prefix",
			uri:     "https://bucket/key",
			wantErr: true,
		},
		{
			name:    "missing key path",
			uri:     "s3://bucket",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
		},
		{
			name:    "only s3://",
			uri:     "s3://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := ParseS3URI(tt.uri)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseS3URI() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseS3URI() unexpected error: %v", err)
				return
			}

			if bucket != tt.wantBucket {
				t.Errorf("ParseS3URI() bucket = %v, want %v", bucket, tt.wantBucket)
			}

			if key != tt.wantKey {
				t.Errorf("ParseS3URI() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

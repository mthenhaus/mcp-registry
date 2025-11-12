package aws

import (
	"testing"
)

func TestParseS3URL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		// S3 URI format (backward compatibility)
		{
			name:       "s3 URI format",
			url:        "s3://my-bucket/path/to/file.json",
			wantBucket: "my-bucket",
			wantKey:    "path/to/file.json",
			wantErr:    false,
		},
		{
			name:       "s3 URI with nested path",
			url:        "s3://bucket-name/deep/nested/path/file.json",
			wantBucket: "bucket-name",
			wantKey:    "deep/nested/path/file.json",
			wantErr:    false,
		},
		{
			name:       "s3 URI single level",
			url:        "s3://bucket/file.json",
			wantBucket: "bucket",
			wantKey:    "file.json",
			wantErr:    false,
		},
		// S3 Object URL formats
		{
			name:       "virtual-hosted-style with region",
			url:        "https://mthenhaus-mcp-registry.s3.us-east-1.amazonaws.com/registry.json",
			wantBucket: "mthenhaus-mcp-registry",
			wantKey:    "registry.json",
			wantErr:    false,
		},
		{
			name:       "virtual-hosted-style us-east-1 (no region)",
			url:        "https://my-bucket.s3.amazonaws.com/path/to/file.json",
			wantBucket: "my-bucket",
			wantKey:    "path/to/file.json",
			wantErr:    false,
		},
		{
			name:       "virtual-hosted-style with nested path",
			url:        "https://bucket-name.s3.eu-west-1.amazonaws.com/deep/nested/path/file.json",
			wantBucket: "bucket-name",
			wantKey:    "deep/nested/path/file.json",
			wantErr:    false,
		},
		{
			name:       "path-style with region",
			url:        "https://s3.us-west-2.amazonaws.com/my-bucket/file.json",
			wantBucket: "my-bucket",
			wantKey:    "file.json",
			wantErr:    false,
		},
		{
			name:       "path-style us-east-1 (no region)",
			url:        "https://s3.amazonaws.com/my-bucket/path/to/file.json",
			wantBucket: "my-bucket",
			wantKey:    "path/to/file.json",
			wantErr:    false,
		},
		{
			name:       "path-style with nested path",
			url:        "https://s3.ap-south-1.amazonaws.com/bucket/deep/nested/file.json",
			wantBucket: "bucket",
			wantKey:    "deep/nested/file.json",
			wantErr:    false,
		},
		{
			name:    "missing https:// prefix",
			url:     "http://bucket.s3.amazonaws.com/key",
			wantErr: true,
		},
		{
			name:    "missing object key",
			url:     "https://bucket.s3.amazonaws.com/",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "only https://",
			url:     "https://",
			wantErr: true,
		},
		{
			name:    "non-S3 URL",
			url:     "https://example.com/file.json",
			wantErr: true,
		},
		{
			name:    "path-style missing key after bucket",
			url:     "https://s3.amazonaws.com/bucket/",
			wantErr: true,
		},
		{
			name:    "path-style missing bucket and key",
			url:     "https://s3.us-east-1.amazonaws.com/",
			wantErr: true,
		},
		{
			name:    "s3 URI missing key",
			url:     "s3://bucket",
			wantErr: true,
		},
		{
			name:    "s3 URI only s3://",
			url:     "s3://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := ParseS3URL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseS3URL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseS3URL() unexpected error: %v", err)
				return
			}

			if bucket != tt.wantBucket {
				t.Errorf("ParseS3URL() bucket = %v, want %v", bucket, tt.wantBucket)
			}

			if key != tt.wantKey {
				t.Errorf("ParseS3URL() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

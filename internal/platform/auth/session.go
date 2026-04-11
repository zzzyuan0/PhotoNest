package auth

import (
	"context"
	"strings"
	"time"
)

type Permission string

const (
	PermissionLibraryRead    Permission = "library.read"
	PermissionLibraryWrite   Permission = "library.write"
	PermissionAssetDownload  Permission = "asset.download"
	PermissionBatchDownload  Permission = "asset.batch_download"
	PermissionLibraryExport  Permission = "library.export"
	PermissionManageProvider Permission = "provider.manage"
	PermissionManagePrivacy  Permission = "privacy.manage"
	PermissionViewDebug      Permission = "debug.read"
	PermissionViewAudit      Permission = "audit.read"
)

type Source string

const (
	SourceNone   Source = ""
	SourceBearer Source = "bearer"
	SourceCookie Source = "cookie"
)

type Subject struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Roles       []string `json:"roles"`
	LibraryIDs  []string `json:"libraryIds"`
}

type Session struct {
	ID              string    `json:"id"`
	SubjectID       string    `json:"subjectId"`
	DisplayName     string    `json:"displayName"`
	Roles           []string  `json:"roles"`
	LibraryIDs      []string  `json:"libraryIds"`
	AuthMethod      string    `json:"authMethod"`
	AuthenticatedAt time.Time `json:"authenticatedAt"`
	RecentAuthAt    time.Time `json:"recentAuthAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CSRFToken       string    `json:"csrfToken"`
}

type Principal struct {
	Session   Session
	Source    Source
	LibraryID string
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func (s Session) Subject() Subject {
	return Subject{
		ID:          s.SubjectID,
		DisplayName: s.DisplayName,
		Roles:       append([]string(nil), s.Roles...),
		LibraryIDs:  append([]string(nil), s.LibraryIDs...),
	}
}

func (s Session) IsAdmin() bool {
	for _, role := range s.Roles {
		if strings.EqualFold(strings.TrimSpace(role), "admin") {
			return true
		}
	}

	return false
}

func (s Session) HasPermission(permission Permission) bool {
	if permission == "" {
		return true
	}
	if s.IsAdmin() {
		return true
	}

	required := strings.TrimSpace(string(permission))
	for _, role := range s.Roles {
		normalized := strings.ToLower(strings.TrimSpace(role))
		if normalized == required {
			return true
		}
		for _, granted := range rolePermissions[normalized] {
			if granted == permission {
				return true
			}
		}
	}

	return false
}

func (s Session) CanAccessLibrary(libraryID string) bool {
	trimmed := strings.TrimSpace(libraryID)
	if trimmed == "" {
		return false
	}
	if s.IsAdmin() {
		return true
	}
	for _, candidate := range s.LibraryIDs {
		if strings.EqualFold(strings.TrimSpace(candidate), trimmed) {
			return true
		}
	}

	return false
}

var rolePermissions = map[string][]Permission{
	"viewer": {
		PermissionLibraryRead,
		PermissionAssetDownload,
	},
	"editor": {
		PermissionLibraryRead,
		PermissionLibraryWrite,
		PermissionAssetDownload,
	},
	"operator": {
		PermissionLibraryRead,
		PermissionLibraryWrite,
		PermissionAssetDownload,
		PermissionBatchDownload,
		PermissionLibraryExport,
	},
	"provider-admin": {
		PermissionManageProvider,
	},
	"privacy-admin": {
		PermissionManagePrivacy,
	},
	"debug-reader": {
		PermissionViewDebug,
	},
	"auditor": {
		PermissionViewAudit,
	},
}

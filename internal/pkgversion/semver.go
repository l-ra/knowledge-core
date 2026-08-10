package pkgversion

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion parses semver major.minor.patch (optional pre-release ignored for matching v1).
func ParseVersion(v string) (major, minor, patch int, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, 0, fmt.Errorf("empty version")
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid semver %q", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, err
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, err
	}
	if major < 0 || minor < 0 || patch < 0 {
		return 0, 0, 0, fmt.Errorf("negative version component")
	}
	return major, minor, patch, nil
}

// MatchesRange checks if version satisfies range: exact, ^X.Y.Z, ~X.Y.Z, >=X.Y.Z, >=X.Y.Z <A.B.C
func MatchesRange(version, rangeSpec string) (bool, error) {
	rangeSpec = strings.TrimSpace(rangeSpec)
	if rangeSpec == "" {
		return false, fmt.Errorf("empty range")
	}
	vm, vn, vp, err := ParseVersion(version)
	if err != nil {
		return false, err
	}

	if strings.HasPrefix(rangeSpec, "^") {
		base := strings.TrimPrefix(rangeSpec, "^")
		bm, bn, bp, err := ParseVersion(base)
		if err != nil {
			return false, err
		}
		if vm != bm {
			return false, nil
		}
		if vn > bn {
			return true, nil
		}
		if vn < bn {
			return false, nil
		}
		return vp >= bp, nil
	}

	if strings.HasPrefix(rangeSpec, "~") {
		base := strings.TrimPrefix(rangeSpec, "~")
		bm, bn, bp, err := ParseVersion(base)
		if err != nil {
			return false, err
		}
		if vm != bm {
			return vm == bm, nil
		}
		if vn != bn {
			return vn == bn, nil
		}
		return vp >= bp, nil
	}

	if strings.Contains(rangeSpec, " ") {
		parts := strings.Fields(rangeSpec)
		for _, p := range parts {
			if strings.HasPrefix(p, ">=") {
				minV := strings.TrimPrefix(p, ">=")
				ok, err := gte(version, minV)
				if err != nil || !ok {
					return false, err
				}
			} else if strings.HasPrefix(p, "<") {
				maxV := strings.TrimPrefix(p, "<")
				ok, err := lt(version, maxV)
				if err != nil || !ok {
					return false, err
				}
			}
		}
		return true, nil
	}

	if strings.HasPrefix(rangeSpec, ">=") {
		return gte(version, strings.TrimPrefix(rangeSpec, ">="))
	}

	// exact
	return version == rangeSpec, nil
}

func gte(a, b string) (bool, error) {
	am, an, ap, err := ParseVersion(a)
	if err != nil {
		return false, err
	}
	bm, bn, bp, err := ParseVersion(b)
	if err != nil {
		return false, err
	}
	if am != bm {
		return am > bm, nil
	}
	if an != bn {
		return an > bn, nil
	}
	return ap >= bp, nil
}

func lt(a, b string) (bool, error) {
	ok, err := gte(a, b)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

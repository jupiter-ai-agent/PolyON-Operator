# PolyON K8s 환경변수/시크릿 관리 종합 검토 보고서

**작성자**: Ganymede  
**작성일**: 2026-03-06  
**대상**: PolyON Operator 환경변수 구조 개선  

## 1. 개요

PolyON 플랫폼은 20여 종의 서비스를 통합 관리하는 복합 플랫폼으로, 현재 각 Kubernetes manifest에서 Go template을 사용하여 환경변수를 하드코딩하고 있습니다. 이로 인해 환경변수 이름 불일치, 중복 관리, 보안 취약성 등의 문제가 발생하고 있어 체계적인 개선이 필요합니다.

## 2. 현재 구조 분석

### 2.1 분석 대상 파일
- `manifests/postgresql.yaml` - 데이터베이스 서비스
- `manifests/redis.yaml` - 캐시 서비스
- `manifests/keycloak.yaml` - 인증 서비스
- `manifests/samba-dc.yaml` - 도메인 컨트롤러
- `manifests/opensearch.yaml` - 검색 엔진
- `manifests/stalwart.yaml` + `stalwart-config.yaml` - 메일 서비스
- `manifests/rustfs.yaml` - 파일 스토리지
- `manifests/core.yaml` - 백엔드 API
- `manifests/console.yaml` - 프론트엔드
- `manifests/ingress.yaml` - 네트워크 라우팅
- `manifests/namespace.yaml` - 네임스페이스

### 2.2 현재 사용 중인 환경변수/템플릿 변수

#### 2.2.1 공통 변수
| 변수명 | 용도 | 사용 서비스 |
|---------|------|-------------|
| `{{.Namespace}}` | K8s 네임스페이스 | 모든 서비스 |
| `{{.AdminPassword}}` | 공통 관리자 비밀번호 | PostgreSQL, Keycloak, Samba DC, Stalwart, RustFS, Core |

#### 2.2.2 도메인 관련 변수 (⚠️ 문제 구간)
| 변수명 | 용도 | 사용 서비스 | 비고 |
|---------|------|-------------|------|
| `{{.Domain}}` | 기본 도메인 (소문자) | Core | example.com |
| `{{.MailDomain}}` | 메일 도메인 | Stalwart config | **불일치** |
| `{{.DomainUpper}}` | 대문자 도메인 | Samba DC | EXAMPLE.COM |
| `{{.DomainNetBIOS}}` | NetBIOS 도메인 | Samba DC, Core | EXAMPLE |
| `{{.DomainDC}}` | LDAP DN 형식 도메인 | Stalwart config | DC=example,DC=com |
| `{{.ConsoleDomain}}` | 콘솔 도메인 | Ingress | **별도 관리** |
| `{{.AuthDomain}}` | 인증 도메인 | Ingress | **별도 관리** |
| `{{.PortalDomain}}` | 포털 도메인 | Ingress | **별도 관리** |

#### 2.2.3 데이터베이스 관련
| 변수명 | 용도 | 사용 서비스 |
|---------|------|-------------|
| `{{.PostgresPassword}}` | PostgreSQL 비밀번호 | PostgreSQL, Keycloak |

#### 2.2.4 특화된 변수
| 변수명 | 용도 | 사용 서비스 |
|---------|------|-------------|
| `SAMBA_ADMIN_PASSWORD` | Samba DC 관리자 | Samba DC (Secret) |
| `RUSTFS_ACCESS_KEY` | RustFS 접근 키 | RustFS (하드코딩: "polyon-admin") |
| `RUSTFS_SECRET_KEY` | RustFS 비밀 키 | RustFS ({{.AdminPassword}} 참조) |

### 2.3 현재 K8s 리소스 사용 현황

#### 2.3.1 Secret 리소스
- `polyon-db-secret` (PostgreSQL 비밀번호)
- `polyon-dc-secret` (Samba 관리자 비밀번호)
- `polyon-rustfs-secret` (RustFS 인증정보)

#### 2.3.2 ConfigMap 리소스
- `polyon-db-init` (PostgreSQL 초기화 스크립트)
- `polyon-mail-config` (Stalwart 메일 설정)
- `polyon-console-nginx` (Console nginx 설정)

## 3. 문제점 식별

### 3.1 환경변수 이름 불일치
**심각도**: 🔴 HIGH

| 문제 유형 | 예시 | 영향 |
|-----------|------|------|
| 도메인 변수 분산 | `{{.Domain}}` vs `{{.MailDomain}}` vs `{{.ConsoleDomain}}` | 도메인 변경 시 다중 수정 필요 |
| 비밀번호 변수명 혼용 | `{{.AdminPassword}}` vs `SAMBA_ADMIN_PASSWORD` | 변경 시 누락 위험 |
| 하드코딩된 값 | `RUSTFS_ACCESS_KEY: "polyon-admin"` | 보안 설정 변경 불가 |

### 3.2 Go Template 방식의 한계
**심각도**: 🟡 MEDIUM

- 빌드 타임에 값이 고정됨
- 런타임 설정 변경 불가
- K8s 네이티브 기능 활용 불가 (Secret rotation, ConfigMap hot reload 등)

### 3.3 보안 취약성
**심각도**: 🔴 HIGH

- 비밀번호가 Manifest 파일에 평문으로 노출 (Go template 처리 후)
- Secret과 ConfigMap의 체계적 분리 부족
- 민감 정보와 일반 설정의 혼재

### 3.4 운영 복잡성
**심각도**: 🟡 MEDIUM

- 설정 변경 시 다수 파일 수정 필요
- 서비스 간 의존성 파악 어려움
- 중복된 설정값의 일관성 보장 어려움

## 4. Core 설정 분석

### 4.1 Core Config 구조 (`config.go`)
Core 서비스는 다음과 같은 설정 계층을 가짐:

1. **환경변수** (최우선)
2. **JSON 파일** (`setup.json`, `smtp.json`)
3. **`.env` 파일**
4. **하드코딩된 기본값**

### 4.2 Core에서 사용하는 서비스 URL
```go
KeycloakURL: "http://polyon-auth:8080"
StalwartURL: "http://polyon-mail:8080"
ElasticURL: "http://polyon-search:9200"
RustFSEndpoint: "http://polyon-rustfs:9000"
// ... 총 15개 이상의 서비스 URL
```

## 5. K8s 네이티브 개선안

### 5.1 설계 원칙

1. **Single Source of Truth**: 공통 설정은 중앙 집중식 관리
2. **Security by Default**: 민감 정보는 K8s Secret으로 격리
3. **Environment Agnostic**: 환경별 설정 오버라이드 지원
4. **Hot Reload Ready**: 런타임 설정 변경 지원

### 5.2 제안하는 리소스 구조

#### 5.2.1 공통 ConfigMap
```yaml
# polyon-common-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: polyon-common-config
  namespace: polyon
data:
  # 도메인 설정 (통합)
  POLYON_DOMAIN: "example.com"
  POLYON_REALM: "EXAMPLE.COM"
  POLYON_NETBIOS_DOMAIN: "EXAMPLE"
  
  # 서비스 엔드포인트
  POLYON_AUTH_URL: "http://polyon-auth:8080"
  POLYON_MAIL_URL: "http://polyon-mail:8080"
  POLYON_SEARCH_URL: "http://polyon-search:9200"
  POLYON_STORAGE_URL: "http://polyon-rustfs:9000"
  POLYON_DB_URL: "polyon-db:5432"
  POLYON_REDIS_URL: "redis://polyon-redis:6379"
  
  # LDAP 설정
  POLYON_LDAP_URL: "ldap://polyon-dc:389"
  POLYON_LDAP_BASE_DN: "DC=example,DC=com"
  
  # 외부 도메인
  POLYON_CONSOLE_DOMAIN: "console.example.com"
  POLYON_AUTH_DOMAIN: "auth.example.com"
  POLYON_PORTAL_DOMAIN: "portal.example.com"
  POLYON_MAIL_DOMAIN: "mail.example.com"
```

#### 5.2.2 공통 Secret
```yaml
# polyon-common-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: polyon-common-secret
  namespace: polyon
type: Opaque
stringData:
  # 공통 관리자 비밀번호
  POLYON_ADMIN_PASSWORD: ""  # 외부에서 주입
  
  # 데이터베이스
  POLYON_DB_PASSWORD: ""
  
  # 서비스별 인증 토큰/키
  POLYON_RUSTFS_ACCESS_KEY: "polyon-admin"
  POLYON_RUSTFS_SECRET_KEY: ""  # ADMIN_PASSWORD와 동일
```

#### 5.2.3 서비스별 ConfigMap (예: Stalwart)
```yaml
# polyon-mail-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: polyon-mail-config
  namespace: polyon
data:
  config.toml: |
    [server]
    hostname = "$(POLYON_MAIL_DOMAIN)"
    
    [storage]
    data = "rocksdb"
    fts = "opensearch"
    
    [store.opensearch]
    type = "elasticsearch"
    url = "$(POLYON_SEARCH_URL)"
    
    [directory.ldap]
    type = "ldap"
    url = "$(POLYON_LDAP_URL)"
    base-dn = "$(POLYON_LDAP_BASE_DN)"
    
    [directory.ldap.bind]
    dn = "CN=Administrator,CN=Users,$(POLYON_LDAP_BASE_DN)"
    secret = "$(POLYON_ADMIN_PASSWORD)"
    
    [authentication.fallback-admin]
    user = "admin"
    secret = "$(POLYON_ADMIN_PASSWORD)"
```

### 5.3 네이밍 컨벤션

#### 5.3.1 환경변수 네이밍
```
POLYON_<CATEGORY>_<ITEM>

예시:
- POLYON_DOMAIN_PRIMARY (기본 도메인)
- POLYON_DOMAIN_MAIL (메일 도메인)
- POLYON_AUTH_URL (인증 서비스 URL)
- POLYON_DB_HOST (데이터베이스 호스트)
- POLYON_ADMIN_PASSWORD (관리자 비밀번호)
```

#### 5.3.2 K8s 리소스 네이밍
```
polyon-<scope>-<type>

예시:
- polyon-common-config (공통 설정)
- polyon-common-secret (공통 시크릿)
- polyon-mail-config (메일 서비스 설정)
- polyon-auth-secret (인증 서비스 시크릿)
```

### 5.4 서비스별 적용 방안

#### 5.4.1 PostgreSQL
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: polyon-db
spec:
  template:
    spec:
      containers:
      - name: postgres
        envFrom:
        - configMapRef:
            name: polyon-common-config
        - secretRef:
            name: polyon-common-secret
        env:
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: polyon-common-secret
              key: POLYON_DB_PASSWORD
```

#### 5.4.2 Core Backend
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: polyon-core
spec:
  template:
    spec:
      containers:
      - name: core
        envFrom:
        - configMapRef:
            name: polyon-common-config
        - secretRef:
            name: polyon-common-secret
        env:
        - name: DATABASE_URL
          value: "postgres://polyon:$(POLYON_DB_PASSWORD)@$(POLYON_DB_URL)/polyon?sslmode=disable"
```

## 6. 마이그레이션 계획

### 6.1 Phase 1: 공통 리소스 생성
**예상 소요시간**: 1일

1. `polyon-common-config` ConfigMap 생성
2. `polyon-common-secret` Secret 생성
3. 기존 템플릿 변수와 신규 환경변수 매핑 테이블 작성

### 6.2 Phase 2: 서비스별 순차 마이그레이션
**예상 소요시간**: 3일

**마이그레이션 우선순위:**
1. PostgreSQL (기반 서비스)
2. Redis (캐시 서비스)
3. Core Backend (중심 서비스)
4. Keycloak (인증 서비스)
5. 기타 서비스들

### 6.3 Phase 3: 기존 템플릿 제거
**예상 소요시간**: 1일

1. Operator 코드에서 Go template 처리 로직 제거
2. 환경변수 기반 배포 로직으로 전환
3. 호환성 테스트 수행

### 6.4 Phase 4: 고도화
**예상 소요시간**: 2일

1. Secret rotation 메커니즘 도입
2. ConfigMap hot reload 구현
3. 환경별 설정 오버라이드 기능

## 7. 기대 효과

### 7.1 운영 측면
- **설정 변경 시간 단축**: 중앙 집중식 관리로 단일 지점 수정
- **장애 복구 시간 단축**: 일관된 네이밍으로 문제 지점 빠른 식별
- **보안 강화**: K8s Secret 활용으로 민감 정보 격리

### 7.2 개발 측면
- **개발 생산성 향상**: 표준화된 환경변수 체계
- **테스트 환경 구축 용이성**: 환경별 설정 오버라이드
- **서비스 간 결합도 감소**: 명확한 의존성 정의

### 7.3 보안 측면
- **Zero Trust 준수**: 모든 비밀번호 Secret 격리
- **Audit Trail 강화**: K8s 네이티브 로깅
- **Secret Rotation 지원**: 런타임 보안 업데이트 가능

## 8. 리스크 및 대응방안

### 8.1 식별된 리스크

| 리스크 | 영향도 | 대응방안 |
|--------|--------|----------|
| 기존 서비스 호환성 문제 | HIGH | Phase-by-phase 마이그레이션, 호환성 테스트 |
| 설정 누락으로 인한 서비스 중단 | HIGH | 상세한 매핑 테이블 작성, 롤백 계획 |
| Operator 코드 수정 범위 확대 | MEDIUM | 점진적 리팩토링, 기능 플래그 활용 |

### 8.2 롤백 계획
1. 각 Phase별 체크포인트 설정
2. 기존 템플릿 방식 백업 유지
3. 문제 발생 시 즉시 이전 버전 복원 가능

## 9. 결론 및 권장사항

PolyON 플랫폼의 환경변수/시크릿 관리 구조는 현재 Go template 방식의 한계로 인해 운영 복잡성과 보안 취약점을 가지고 있습니다. 

**핵심 권장사항:**

1. **즉시 시행**: 공통 ConfigMap/Secret 도입으로 중앙 집중식 관리
2. **단계적 마이그레이션**: 서비스별 순차 적용으로 리스크 최소화
3. **표준화**: 일관된 네이밍 컨벤션으로 운영 효율성 확보
4. **보안 강화**: K8s 네이티브 Secret 관리 체계 구축

이러한 개선을 통해 PolyON 플랫폼의 운영 안정성, 보안성, 그리고 개발 생산성을 크게 향상시킬 수 있을 것입니다.

---

**검토 완료**: 2026-03-06  
**다음 단계**: 개발팀 검토 후 Phase 1 구현 착수  
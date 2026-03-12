# PolyON Console — Carbon DataTable 준수 감사 리포트

**작성일:** 2026-03-06  
**작성자:** Jupiter (팀장)  
**비교 기준:** IBM Cloud UI + Carbon Design System DataTable 가이드라인  
**참조:** https://react.carbondesignsystem.com/ / https://carbondesignsystem.com/components/data-table/usage/

---

## 1. 요약

| 항목 | IBM Cloud (기준) | PolyON Console (현재) | 일치 여부 |
|------|-----------------|---------------------|-----------|
| 전체 선택 (Select All) | ✅ 체크박스, 3상태(all/none/indeterminate) | ✅ `TableSelectAll` 사용 | ✅ 일치 |
| 행 선택 (Select Row) | ✅ 체크박스 | ✅ `TableSelectRow` 사용 | ✅ 일치 |
| 검색 (Search) | ✅ collapsed → 아이콘 클릭 시 확장 | ⚠️ `persistent` 항상 열림 | ⚠️ 차이 |
| 필터 (Filter) | ✅ 드롭다운 필터 (예: 지역) | ⚠️ ghost 아이콘만 있고 기능 미구현 | ❌ 미구현 |
| 컬럼 선택 (열 편집) | ✅ 설정(⚙) 아이콘 → 체크박스 패널 | ❌ 없음 | ❌ 누락 |
| CSV 다운로드 | ✅ 다운로드(↓) 아이콘 | ✅ Download 아이콘 | ✅ 일치 |
| 새로고침 | ✅ 새로고침(↻) 아이콘 | ✅ Renew 아이콘 | ✅ 일치 |
| Primary 버튼 위치/스타일 | ✅ 우측 끝, `primary` kind, 아이콘+텍스트 | ⚠️ `size="sm"` 사용 | ⚠️ 크기 차이 |
| 탭 (Tab) 사용 | ✅ Contained Tab, 테이블 위 | ⚠️ 일부 페이지만 사용, 스타일 불일치 | ⚠️ 부분 |
| Batch Actions | ✅ 선택 시 상단 바 표시 | ✅ `TableBatchActions` 사용 | ✅ 일치 |
| 정렬 (Sort) | ✅ 헤더 클릭, arrow 아이콘 | ✅ `getHeaderProps` 기본 정렬 | ✅ 일치 |
| 빈 상태 (Empty State) | ✅ 일러스트 + 설명 + CTA 링크 | ⚠️ 텍스트만 ("사용자가 없습니다") | ⚠️ 차이 |
| Pagination | ✅ 하단, 페이지 크기 변경 가능 | ✅ `Pagination` 컴포넌트 사용 | ✅ 일치 |
| Overflow Menu (행 액션) | ✅ 각 행 우측 OverflowMenu | ✅ `OverflowMenu` 사용 | ✅ 일치 |
| 로딩 상태 | ✅ Skeleton 로딩 | ⚠️ `DataTableSkeleton` 사용 (일부 페이지) | ⚠️ 부분 |
| Hover 상태 | ✅ 행 hover 항상 활성 | ✅ CSS 기본 적용 | ✅ 일치 |
| Zebra Stripe | ✅ 선택적 사용 | ❌ 미사용 | ⚠️ 선택사항 |

---

## 2. 상세 분석

### 2.1 🔴 컬럼 선택 기능 (열 편집) — 누락

**IBM Cloud:** 툴바 우측 설정(⚙) 아이콘 클릭 → "열 편집" 패널 표시. 각 컬럼을 체크박스로 표시/숨김 제어. "기본값으로 재설정" 버튼 포함.

**PolyON:** 해당 기능 없음. 모든 컬럼이 항상 표시됨.

**Carbon 컴포넌트:** `@carbon/react`에는 빌트인 컬럼 선택 기능이 없음 → IBM Cloud 자체 구현 (`Carbon for Cloud` 확장). PolyON도 커스텀 구현 필요.

**구현 방안:**
```tsx
// TableToolbarContent 안에 Settings 아이콘 버튼 추가
<Button kind="ghost" renderIcon={Settings} hasIconOnly 
  iconDescription="열 편집" onClick={() => setColumnPanelOpen(true)} />

// OverflowMenu 또는 별도 패널로 컬럼 체크박스 목록 표시
```

### 2.2 🟡 검색 스타일 — 차이

**IBM Cloud:** 검색이 collapsed 상태(돋보기 아이콘만)에서 시작, 클릭하면 확장. 검색어 입력 후 테이블 필터링.

**PolyON:** `persistent` 속성으로 항상 열려있음. 검색 동작은 정상.

**Carbon 가이드라인:** 두 가지 모두 유효 — "collapsed search"와 "open search". IBM Cloud는 collapsed 선호.

**권장:** 대부분의 PolyON 테이블에서 `persistent` 제거하면 IBM Cloud 스타일과 동일.

### 2.3 🟡 Primary 버튼 크기/스타일 — 차이

**IBM Cloud:** "작성 +" 버튼 → `kind="primary"`, 아이콘(+) 포함, **기본 크기(md)**. Tall toolbar와 짝.

**PolyON:** `size="sm"`, `kind` 미지정(기본 primary). 

**Carbon 가이드라인:**
> "The tall toolbar should only be paired with the large and extra large row heights and the small toolbar should only be used with the small and extra small row heights."

| 항목 | IBM Cloud | PolyON |
|------|-----------|--------|
| 테이블 row size | `md` (기본) | `md` |
| Toolbar size | `lg` (tall) | `sm` (small) |
| Primary 버튼 size | `md` 또는 미지정(기본) | `sm` |

**문제:** `md` row height에 `sm` 툴바/버튼 → Carbon 가이드라인 위반.  
**수정:** Primary 버튼에서 `size="sm"` 제거 (기본값 `md` 사용).

### 2.4 🟡 탭 (Contained Tabs) — 부분 준수

**IBM Cloud:** 테이블 위에 **Contained Tab** 사용 (예: "전용 호스트" / "전용 그룹"). 탭 아래에 지역 필터 + 검색 + 툴바.

**PolyON:** 일부 페이지에서 `Tab`/`TabList` 사용하지만, **Contained** variant 미사용. 

**Carbon:**
```tsx
<Tabs>
  <TabList contained>  {/* ← contained 속성 필수 */}
    <Tab>전용 호스트</Tab>
    <Tab>전용 그룹</Tab>
  </TabList>
  <TabPanels>...</TabPanels>
</Tabs>
```

**수정:** `<TabList contained>` 속성 추가.

### 2.5 🔴 필터 기능 — 미구현

**IBM Cloud:** 드롭다운 필터 (예: "지역: 프랑크푸르트 (eu-de)"). 테이블 위에 위치.

**PolyON:** Filter 아이콘만 있고 클릭 시 동작 없음.

**구현 방안:** `Dropdown` 또는 `MultiSelect` 컴포넌트를 툴바 좌측에 배치.

### 2.6 🟡 빈 상태 (Empty State) — 차이

**IBM Cloud:** 
- 일러스트 이미지 (서버 아이콘)
- 제목: "가상 서버 인스턴스 없음"
- 설명: "가상 서버 인스턴스 목록이 비어 있습니다..."
- CTA 링크: "자세히 보기 ↗"

**PolyON:** 텍스트만 ("사용자가 없습니다"), 중앙 정렬.

**수정:**
```tsx
// Empty state 공통 컴포넌트 필요
<EmptyState
  icon={<UserMultiple size={64} />}
  title="사용자가 없습니다"
  description="시작하려면 "Add User"를 클릭하십시오."
  action={<Link href="#">자세히 보기</Link>}
/>
```

### 2.7 ✅ 잘 지키고 있는 항목

| 항목 | 상세 |
|------|------|
| **DataTable render prop 패턴** | `@carbon/react`의 `DataTable` 컴포넌트를 render prop으로 정확히 사용 |
| **TableContainer 구조** | Toolbar → Table → Pagination 순서 올바름 |
| **Select All / Select Row** | `TableSelectAll` + `TableSelectRow` 올바른 사용 |
| **Batch Actions** | 선택 시 `TableBatchActions` 바 표시, 삭제 등 batch action 제공 |
| **OverflowMenu** | 각 행에 `OverflowMenu` + `OverflowMenuItem` 올바른 사용 |
| **Pagination** | `Pagination` 컴포넌트, `pageSizes` 옵션 제공 |
| **Carbon Icons** | `@carbon/icons-react`에서 올바른 아이콘 import |
| **Token 기반 색상** | `var(--cds-text-primary)` 등 Carbon token 사용 |
| **IBM Plex Sans** | 전체 폰트 IBM Plex Sans 적용 |

---

## 3. 우선순위별 수정 목록

### P0 — 즉시 수정 (Carbon 가이드라인 위반)

| # | 항목 | 현재 | 수정 | 영향 범위 |
|---|------|------|------|-----------|
| 1 | Primary 버튼 크기 | `size="sm"` | `size` prop 제거 (기본 md) | 전 페이지 (~40개) |
| 2 | Toolbar 높이 | sm (row md와 불일치) | lg 또는 기본 (row md와 매칭) | 전 페이지 |

### P1 — 구조적 개선 (IBM Cloud 패턴 준수)

| # | 항목 | 설명 | 작업량 |
|---|------|------|--------|
| 3 | 컬럼 선택 (열 편집) | Settings 아이콘 → 체크박스 패널 | 공통 컴포넌트 1개 + 각 페이지 적용 |
| 4 | Contained Tabs | `<TabList contained>` 적용 | 탭 사용 페이지 전체 |
| 5 | Empty State 컴포넌트 | 아이콘 + 제목 + 설명 + CTA | 공통 컴포넌트 1개 + 적용 |
| 6 | 필터 드롭다운 | 주요 테이블에 필터 기능 구현 | 페이지별 개별 구현 |

### P2 — 세부 개선 (UX 품질)

| # | 항목 | 설명 |
|---|------|------|
| 7 | 검색 collapsed 기본 | `persistent` 제거하여 collapsed 검색 |
| 8 | CSV 다운로드 비활성화 | 데이터 0건일 때 `disabled` 처리 |
| 9 | Zebra stripe 선택적 적용 | 컬럼 많은 테이블에 적용 |
| 10 | Skeleton 로딩 통일 | 모든 테이블 페이지에 `DataTableSkeleton` 적용 |

---

## 4. IBM Cloud 툴바 구조 정확한 분석

```
┌─────────────────────────────────────────────────────────────────┐
│ [지역: 프랑크푸르트 ▼]  🔍 항목 검색    ↻  ↓  ⚙  [작성 +]  │  ← Toolbar
├──┬────────┬────────┬──────────┬──────────┬────────┬─────┬──────┤
│☐ │ 이름   │ 상태   │ 리소스 그룹↕│ VPC      │프로파일│예약IP│유동IP│  ← Header
├──┼────────┼────────┼──────────┼──────────┼────────┼─────┼──────┤
│  │                  (빈 상태 일러스트)                          │
└──┴────────────────────────────────────────────────────────────────┘
```

**Toolbar 아이콘 순서 (좌→우):**
1. 드롭다운 필터 (좌측)
2. 검색 (collapsed)
3. 새로고침 (↻) — ghost 아이콘
4. 다운로드 (↓) — ghost 아이콘
5. 설정/열 편집 (⚙) — ghost 아이콘
6. Primary 버튼 ("작성 +") — 우측 끝

**PolyON 현재 순서:**
1. 검색 (persistent, 좌측)
2. 필터 아이콘 (미구현)
3. 새로고침 (↻)
4. 다운로드 (↓)
5. Primary 버튼 ("Add User +") — `size="sm"`

**차이점:** 설정(⚙) 아이콘 누락, Primary 버튼 크기, 필터 드롭다운 미구현.

---

## 5. 결론

PolyON Console은 **Carbon DataTable의 핵심 구조(render prop 패턴, 컴포넌트 계층)는 올바르게 사용**하고 있습니다. `@carbon/react`에서 제공하는 `DataTable`, `TableToolbar`, `TableSelectAll`, `Pagination` 등을 정확히 import하고 사용합니다.

그러나 **IBM Cloud 수준의 완성도**와 비교하면:

1. **컬럼 선택 기능 완전 누락** — IBM Cloud의 핵심 UX 패턴
2. **버튼/툴바 크기 불일치** — Carbon sizing 가이드라인 위반
3. **필터 기능 미구현** — 아이콘만 존재
4. **빈 상태 디자인 부실** — 텍스트만, IBM Cloud는 일러스트+CTA

이 4가지를 수정하면 IBM Cloud와 동일한 DataTable 품질을 달성할 수 있습니다.

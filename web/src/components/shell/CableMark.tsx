// The three-color cable mini-mark (board3 `.logo` svg). Also used as the
// favicon (index.html, static data-URI) and the no-project hero (NoProjects).
export function CableMark() {
  return (
    <svg width="22" height="14" viewBox="0 0 30 18" aria-hidden="true">
      <path d="M1 4 C10 4, 14 9, 29 9" stroke="#ECC678" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M1 9 L29 9" stroke="#93B4F2" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M1 14 C10 14, 14 9, 29 9" stroke="#7BD8A0" strokeWidth="2" fill="none" strokeLinecap="round" />
    </svg>
  );
}

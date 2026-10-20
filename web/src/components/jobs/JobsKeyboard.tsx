import { useEffect, useId, useRef, useState, type RefObject } from "react";
import { Button } from "../ui/Button";

export function JobsKeyboard({ list, search }: {
  list: RefObject<HTMLDivElement | null>;
  search: RefObject<HTMLInputElement | null>;
}) {
  const [help, setHelp] = useState(false);
  const id = useId();
  const hint = useRef<HTMLDivElement>(null);
  useEffect(() => {
    function keydown(event: KeyboardEvent) {
      const target = event.target instanceof Element ? event.target : document.activeElement;
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.isComposing ||
        target?.closest('input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"]') ||
        list.current?.closest("[inert]") || document.querySelector('[aria-modal="true"]')) return;
      if (event.key === "?" || (help && event.key === "Escape")) {
        event.preventDefault();
        setHelp(event.key === "?" ? !help : false);
        if (help) hint.current?.querySelector("button")?.focus();
        return;
      }
      if (help) return;
      if (event.key === "/") {
        event.preventDefault();
        search.current?.focus();
        return;
      }
      const links = [...(list.current?.querySelectorAll<HTMLAnchorElement>("[data-job-link]") || [])]
        .filter(link => !link.closest("[inert],[hidden]"));
      const index = links.indexOf(document.activeElement as HTMLAnchorElement);
      if (event.key === "j" || event.key === "k") {
        if (!links.length) return;
        event.preventDefault();
        const next = index < 0 ? (event.key === "j" ? 0 : links.length - 1)
          : Math.max(0, Math.min(links.length - 1, index + (event.key === "j" ? 1 : -1)));
        links[next].focus({preventScroll:true});
        links[next].scrollIntoView({block:"nearest"});
      } else if (event.key === "Enter" && index >= 0) {
        event.preventDefault();
        location.hash = links[index].hash;
      }
    }
    function outside(event: PointerEvent) {
      if (!hint.current?.contains(event.target as Node)) setHelp(false);
    }
    window.addEventListener("keydown", keydown);
    document.addEventListener("pointerdown", outside);
    return () => {
      window.removeEventListener("keydown", keydown);
      document.removeEventListener("pointerdown", outside);
    };
  }, [help, list, search]);
  return <div className="jobs-keyboard" ref={hint}>
    <Button size="sm" variant="ghost" aria-keyshortcuts="?" aria-expanded={help} aria-controls={id} onClick={() => setHelp(!help)}>
      <kbd>?</kbd> Keyboard shortcuts
    </Button>
    {help && <section id={id} className="jobs-keyboard-help" aria-label="Keyboard shortcuts">
      <h2>Keyboard shortcuts</h2>
      <dl>
        <dt><kbd>/</kbd></dt><dd>Focus search</dd>
        <dt><kbd>j</kbd> / <kbd>k</kbd></dt><dd>Next / previous job</dd>
        <dt><kbd>Enter</kbd></dt><dd>Open details</dd>
        <dt><kbd>Esc</kbd></dt><dd>Close details or help</dd>
        <dt><kbd>?</kbd></dt><dd>Toggle this help</dd>
      </dl>
      <Button size="sm" variant="ghost" onClick={() => {setHelp(false); hint.current?.querySelector("button")?.focus();}}>Close keyboard shortcuts</Button>
    </section>}
  </div>;
}

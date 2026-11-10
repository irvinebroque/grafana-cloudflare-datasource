import React, { useCallback, useEffect, useRef } from "react";
import { CodeEditor, type CodeEditorSuggestionItem } from "@grafana/ui";

interface Props {
  label: string;
  language: "sql" | "graphql" | "json";
  value: string;
  height: number;
  onChange: (value: string) => void;
  onRunQuery: () => void;
  getSuggestions?: () => CodeEditorSuggestionItem[];
}

export function QueryCodeEditor({
  label,
  language,
  value,
  height,
  onChange,
  onRunQuery,
  getSuggestions,
}: Props) {
  const change = useRef(onChange);

  useEffect(() => {
    change.current = onChange;
  }, [onChange]);

  // Grafana registers this listener once when the Monaco model mounts.
  const update = useCallback((text: string) => change.current(text), []);

  return (
    <div
      style={{ width: "100%", minWidth: 0 }}
      onKeyDownCapture={(event) => {
        if (event.key === "Enter" && (event.shiftKey || event.ctrlKey || event.metaKey)) {
          event.preventDefault();
          event.stopPropagation();
          onRunQuery();
        }
      }}
    >
      <CodeEditor
        value={value}
        language={language}
        height={height}
        showLineNumbers
        showMiniMap={false}
        wordWrap
        monacoOptions={{ ariaLabel: label, scrollBeyondLastLine: false }}
        onChange={update}
        getSuggestions={getSuggestions}
      />
      <small>Shift+Enter or Ctrl/Cmd+Enter to run</small>
    </div>
  );
}

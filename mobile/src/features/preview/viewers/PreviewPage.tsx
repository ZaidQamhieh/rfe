import { memo, useCallback } from 'react';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { previewKindOf } from '../previewKind';
import { ArchiveViewer } from './ArchiveViewer';
import { CsvViewer } from './CsvViewer';
import { ImageViewer } from './ImageViewer';
import { MarkdownViewer } from './MarkdownViewer';
import { AudioViewer, VideoViewer } from './MediaViewers';
import { PdfViewer } from './PdfViewer';
import { TextViewer } from './TextViewer';

type Props = {
  host: Host;
  entry: Entry;
  isCurrent: boolean;
  lineNumbers: boolean;
  rawMarkdown: boolean;
  follow: boolean;
  onZoomChange: (zoomed: boolean) => void;
  onText: (path: string, text: string | null) => void;
};

/** Port of _viewerFor: the chromeless per-kind viewer for one pager page. */
export const PreviewPage = memo(function PreviewPage({ host, entry, isCurrent, lineNumbers, rawMarkdown, follow, onZoomChange, onText }: Props) {
  const reportText = useCallback((text: string | null) => onText(entry.path, text), [onText, entry.path]);
  switch (previewKindOf(entry)) {
    case 'image':
      return <ImageViewer host={host} entry={entry} onZoomChange={isCurrent ? onZoomChange : undefined} />;
    case 'text':
      return <TextViewer host={host} entry={entry} showLineNumbers={lineNumbers} follow={follow && isCurrent} onText={reportText} />;
    case 'markdown':
      return <MarkdownViewer host={host} entry={entry} raw={rawMarkdown} onText={reportText} />;
    case 'csv':
      return <CsvViewer host={host} entry={entry} />;
    case 'archive':
      return <ArchiveViewer host={host} entry={entry} />;
    case 'pdf':
      return <PdfViewer host={host} entry={entry} />;
    case 'video':
      return <VideoViewer host={host} entry={entry} isCurrent={isCurrent} />;
    case 'audio':
      return <AudioViewer host={host} entry={entry} isCurrent={isCurrent} />;
    case 'none':
      return null;
  }
});

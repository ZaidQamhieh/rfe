import { View } from 'react-native';

/** Four L-shaped corner brackets (the mockup's QR viewfinder chrome). Shared by the pairing and receive scanners. */
export function Brackets({ size, width, inset, color }: { size: number; width: number; inset: number; color: string }) {
  const corner = (top: boolean, left: boolean) => (
    <View
      key={`${top}${left}`}
      style={{
        position: 'absolute',
        [top ? 'top' : 'bottom']: inset,
        [left ? 'left' : 'right']: inset,
        width: size,
        height: size,
        borderColor: color,
        [top ? 'borderTopWidth' : 'borderBottomWidth']: width,
        [left ? 'borderLeftWidth' : 'borderRightWidth']: width,
      }}
    />
  );
  return <>{[corner(true, true), corner(true, false), corner(false, true), corner(false, false)]}</>;
}

import AnsiToHtml from 'ansi-to-html';

// Ansi palette legible on the light terminal surface
const LIGHT_COLORS: Record<number, string> = {
	0: '#000000',
	1: '#cd3131',
	2: '#00a600',
	3: '#949800',
	4: '#0451a5',
	5: '#bc05bc',
	6: '#0598bc',
	7: '#555555',
	8: '#666666',
	9: '#cd3131',
	10: '#14ce14',
	11: '#b5ba00',
	12: '#0451a5',
	13: '#bc05bc',
	14: '#0598bc',
	15: '#a5a5a5'
};

// Builds a converter whose colors match the active theme
export function themedAnsiConverter(mode: string | undefined) {
	const light = mode === 'light';
	return new AnsiToHtml({
		fg: light ? '#3f3f46' : '#e8e8e8',
		bg: light ? '#f4f4f5' : '#000000',
		colors: light ? LIGHT_COLORS : {},
		newline: false,
		escapeXML: true,
		stream: true
	});
}

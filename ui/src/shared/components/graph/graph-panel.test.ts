import {getZoomPercentage, zoomNodeSize} from './graph-panel';

describe('getZoomPercentage', () => {
    it('returns 100% at the base node size', () => {
        expect(getZoomPercentage(64, 64)).toBe(100);
    });

    it('returns the zoom level relative to the base node size', () => {
        expect(getZoomPercentage(70.4, 64)).toBe(110);
        expect(getZoomPercentage(57.6, 64)).toBe(90);
    });

    it('rounds to the nearest whole percentage', () => {
        expect(getZoomPercentage(64.5, 64)).toBe(101);
        expect(getZoomPercentage(63.5, 64)).toBe(99);
    });
});

describe('zoomNodeSize', () => {
    const baseNodeSize = 64;

    it('increments by a fixed 10% step when zooming in', () => {
        expect(zoomNodeSize(64, baseNodeSize, 1)).toBe(70.4);
        expect(zoomNodeSize(70.4, baseNodeSize, 1)).toBeCloseTo(76.8);
    });

    it('decrements by a fixed 10% step when zooming out', () => {
        expect(zoomNodeSize(64, baseNodeSize, -1)).toBe(57.6);
        expect(zoomNodeSize(57.6, baseNodeSize, -1)).toBe(51.2);
    });

    it('returns exactly the base size after zooming in and back out', () => {
        expect(zoomNodeSize(zoomNodeSize(64, baseNodeSize, 1), baseNodeSize, -1)).toBe(64);
    });

    it('clamps at the 10% minimum zoom level', () => {
        expect(zoomNodeSize(6.4, baseNodeSize, -1)).toBe(6.4);
    });
});

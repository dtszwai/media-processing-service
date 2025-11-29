import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import {
  mediaList,
  currentMediaId,
  isProcessing,
  apiConnected,
  updateMediaStatus,
  updateMediaWidth,
  addMedia,
  removeMedia,
} from './stores';
import type { Media } from './types';

const createMedia = (id: string, overrides?: Partial<Media>): Media => ({
  mediaId: id,
  name: `test-${id}.jpg`,
  size: 1024,
  mimetype: 'image/jpeg',
  status: 'PENDING',
  width: 500,
  ...overrides,
});

describe('stores', () => {
  beforeEach(() => {
    mediaList.set([]);
    currentMediaId.set(null);
    isProcessing.set(false);
    apiConnected.set(false);
  });

  describe('mediaList', () => {
    it('starts empty', () => {
      expect(get(mediaList)).toEqual([]);
    });

    it('can be set directly', () => {
      const media = [createMedia('1'), createMedia('2')];
      mediaList.set(media);
      expect(get(mediaList)).toHaveLength(2);
    });
  });

  describe('addMedia', () => {
    it('adds media to the beginning of the list', () => {
      addMedia(createMedia('1'));
      addMedia(createMedia('2'));
      const list = get(mediaList);
      expect(list[0].mediaId).toBe('2');
      expect(list[1].mediaId).toBe('1');
    });
  });

  describe('removeMedia', () => {
    it('removes media by id', () => {
      addMedia(createMedia('1'));
      addMedia(createMedia('2'));
      removeMedia('1');
      const list = get(mediaList);
      expect(list).toHaveLength(1);
      expect(list[0].mediaId).toBe('2');
    });

    it('does nothing if id not found', () => {
      addMedia(createMedia('1'));
      removeMedia('nonexistent');
      expect(get(mediaList)).toHaveLength(1);
    });
  });

  describe('updateMediaStatus', () => {
    it('updates status of existing media', () => {
      addMedia(createMedia('1', { status: 'PENDING' }));
      updateMediaStatus('1', 'COMPLETE');
      expect(get(mediaList)[0].status).toBe('COMPLETE');
    });

    it('does nothing if id not found', () => {
      addMedia(createMedia('1', { status: 'PENDING' }));
      updateMediaStatus('nonexistent', 'COMPLETE');
      expect(get(mediaList)[0].status).toBe('PENDING');
    });
  });

  describe('updateMediaWidth', () => {
    it('updates width of existing media', () => {
      addMedia(createMedia('1', { width: 500 }));
      updateMediaWidth('1', 800);
      expect(get(mediaList)[0].width).toBe(800);
    });
  });

  describe('currentMediaId', () => {
    it('starts as null', () => {
      expect(get(currentMediaId)).toBeNull();
    });

    it('can be set', () => {
      currentMediaId.set('test-id');
      expect(get(currentMediaId)).toBe('test-id');
    });
  });

  describe('isProcessing', () => {
    it('starts as false', () => {
      expect(get(isProcessing)).toBe(false);
    });
  });

  describe('apiConnected', () => {
    it('starts as false', () => {
      expect(get(apiConnected)).toBe(false);
    });
  });
});

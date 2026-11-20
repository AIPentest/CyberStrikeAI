/**
 * 对话附件（chat_uploads）接口封装：列表、上传、目录、内容、下载、导出与路径解析。
 */
import { api, downloadFile } from '../../lib/http';

export function listFiles(query) {
    return api.get('/chat-uploads', query);
}

export function uploadFile(file, { relativeDir } = {}) {
    const form = new FormData();
    form.append('file', file, file.name);
    if (relativeDir) form.append('relativeDir', relativeDir);
    return api.post('/chat-uploads', form);
}

export function makeDirectory(parent, name) {
    return api.post('/chat-uploads/mkdir', { parent: parent || '', name });
}

export function deleteFile(path) {
    return api.del('/chat-uploads', { body: { path } });
}

export function getContent(path) {
    return api.get('/chat-uploads/content', { path });
}

export function resolvePath(path, kind = 'file') {
    return api.get('/chat-uploads/path', { path, kind });
}

export function exportFiles(query) {
    return downloadFile('/chat-uploads/export', { query, filename: 'chat-files-export.zip' });
}

export function downloadFileByPath(path, filename) {
    return downloadFile('/chat-uploads/download', { query: { path }, filename });
}

/** 取二进制内容（图片预览用），返回 objectURL，调用方负责 revoke。 */
export async function fetchBlobUrl(path) {
    const response = await api.raw('/chat-uploads/download', { query: { path }, timeout: 120000 });
    const blob = await response.blob();
    return { url: URL.createObjectURL(blob), type: blob.type, size: blob.size };
}

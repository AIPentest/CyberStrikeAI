/** 会话级写操作：新建、重命名、置顶、删除、批量删除、删除某一轮。 */
import { useCallback } from 'react';
import { api } from '../../lib/http';
import { confirmAction, toastError, toastSuccess, toastWarning } from '../../components/ui';

export function useConversationActions({
    t,
    conversationRef,
    newProjectId,
    canWrite,
    reloadList,
    requestDetailReload,
    navigate,
    onSelectionCleared
}) {
    const createConversation = useCallback(async () => {
        if (!canWrite) return;
        try {
            const payload = await api.post('/conversations', {
                title: t('chat.newChat', null, '新对话'),
                projectId: newProjectId || undefined
            });
            const id = payload?.id;
            reloadList();
            if (id) navigate({ conversation: id });
            toastSuccess(t('chat.created', null, '已创建对话'));
        } catch (error) {
            toastError(error, t('chat.createFailed', null, '创建对话失败'));
        }
    }, [canWrite, navigate, newProjectId, reloadList, t]);

    const renameConversation = useCallback(async (id, title) => {
        try {
            await api.put(`/conversations/${encodeURIComponent(id)}`, { title });
            toastSuccess(t('chat.renamed', null, '已重命名'));
            reloadList();
            if (id === conversationRef.current) requestDetailReload(id, { silent: true });
        } catch (error) {
            toastError(error, t('chat.renameFailed', null, '重命名失败'));
        }
    }, [conversationRef, reloadList, requestDetailReload, t]);

    const togglePin = useCallback(async item => {
        try {
            await api.put(`/conversations/${encodeURIComponent(item.id)}/pinned`, { pinned: !item.pinned });
            reloadList();
        } catch (error) {
            toastError(error, t('chat.pinFailed', null, '置顶操作失败'));
        }
    }, [reloadList, t]);

    const deleteConversation = useCallback(async item => {
        try {
            await api.del(`/conversations/${encodeURIComponent(item.id)}`);
            toastSuccess(t('chat.deleted', null, '已删除对话'));
            if (item.id === conversationRef.current) {
                onSelectionCleared();
                navigate({}, { replace: true });
            }
            reloadList();
        } catch (error) {
            toastError(error, t('chat.deleteFailed', null, '删除对话失败'));
        }
    }, [conversationRef, navigate, onSelectionCleared, reloadList, t]);

    const batchDelete = useCallback(async ids => {
        const results = await Promise.allSettled(ids.map(id => api.del(`/conversations/${encodeURIComponent(id)}`)));
        const failed = results.filter(result => result.status === 'rejected').length;
        if (failed) toastWarning(t('chat.batchDeletePartial', { failed }, `有 ${failed} 个对话删除失败`));
        else toastSuccess(t('chat.batchDeleted', { count: ids.length }, `已删除 ${ids.length} 个对话`));
        if (ids.includes(conversationRef.current)) {
            onSelectionCleared();
            navigate({}, { replace: true });
        }
        reloadList();
    }, [conversationRef, navigate, onSelectionCleared, reloadList, t]);

    const deleteTurn = useCallback(message => {
        const id = conversationRef.current;
        if (!id || !message?.id) return;
        confirmAction({
            title: t('chat.deleteTurnTitle', null, '删除本轮'),
            content: t('chat.deleteTurnConfirm', null, '将删除该轮提问与全部回答，删除后不可恢复。'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.post(`/conversations/${encodeURIComponent(id)}/delete-turn`, { messageId: message.id });
                    toastSuccess(t('chat.turnDeleted', null, '已删除该轮'));
                    await requestDetailReload(id, { silent: true });
                    reloadList();
                } catch (error) {
                    toastError(error, t('chat.deleteTurnFailed', null, '删除轮次失败'));
                }
            }
        });
    }, [conversationRef, reloadList, requestDetailReload, t]);

    return { createConversation, renameConversation, togglePin, deleteConversation, batchDelete, deleteTurn };
}

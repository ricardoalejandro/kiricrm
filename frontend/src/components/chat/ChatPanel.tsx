'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import {
  Send, Paperclip, MoreVertical, Search, Phone, Video,
  ArrowLeft, Smile, Image as ImageIcon, FileText, X,
  Mic, Trash2, Reply, Check, CheckCheck, Download,
  CornerUpRight, Play, Pause, AlertCircle, User, EyeOff, RefreshCw
} from 'lucide-react'
import { format } from 'date-fns'
import { es } from 'date-fns/locale'
import { Chat, Message } from '@/types/chat'
import { subscribeWebSocket } from '@/lib/api'
import { getChatDisplayName } from '@/utils/chat'
import WhatsAppTextInput, { WhatsAppTextInputHandle } from '../WhatsAppTextInput'
import ImageViewer from './ImageViewer'
import MessageBubble from './MessageBubble'
import StickerPicker from './StickerPicker'
import EmojiPicker from './EmojiPicker'
import ContactPanel from './ContactPanel'
import ForwardMessageModal from './ForwardMessageModal'
import QuickReplyPicker from './QuickReplyPicker'
import ContactSelector, { SelectedPerson } from '../ContactSelector'
import { compressImageStandard } from '@/utils/imageCompression'

type CachedChatMessages = {
  messages: Message[]
  hasMore: boolean
}

interface ChatPanelProps {
  chatId: string | null
  deviceId?: string
  initialChat?: Chat
  onClose?: () => void
  className?: string
  readOnly?: boolean
  onContactInfoToggle?: (show: boolean) => void
  contactInfoOpen?: boolean
}

export default function ChatPanel({ chatId, deviceId, initialChat, onClose, className = '', readOnly = false, onContactInfoToggle, contactInfoOpen }: ChatPanelProps) {
  const [chat, setChat] = useState<Chat | null>(initialChat || null)
  const [messages, setMessages] = useState<Message[]>([])
  const messagesCacheRef = useRef<Map<string, CachedChatMessages>>(new Map())
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMoreMessages, setHasMoreMessages] = useState(true)
  const [messageText, setMessageText] = useState('')
  const [sendingMessage, setSendingMessage] = useState(false)
  const [replyingTo, setReplyingTo] = useState<Message | null>(null)

  // Attachments
  const [showAttachments, setShowAttachments] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const docFileInputRef = useRef<HTMLInputElement>(null)
  const [showContactPicker, setShowContactPicker] = useState(false)

  // Media preview with caption
  const [pendingMedia, setPendingMedia] = useState<{ file: File; type: string; previewUrl: string } | null>(null)
  const [mediaCaption, setMediaCaption] = useState('')

  // Modals & Viewers
  const [viewImage, setViewImage] = useState<string | null>(null)
  const [activePopup, setActivePopup] = useState<'emoji' | 'sticker' | null>(null)

  // Panels
  const [showContactInfoLocal, setShowContactInfoLocal] = useState(false)
  const showContactInfo = contactInfoOpen !== undefined ? contactInfoOpen : showContactInfoLocal
  const setShowContactInfo = (show: boolean) => {
    if (onContactInfoToggle) onContactInfoToggle(show)
    else setShowContactInfoLocal(show)
  }
  const [showSearch, setShowSearch] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')

  // Forwarding
  const [forwardingMsg, setForwardingMsg] = useState<Message | null>(null)
  const [forwardSearch, setForwardSearch] = useState('')

  // Editing
  const [editingMsg, setEditingMsg] = useState<Message | null>(null)

  // Quick Reply
  const [showQuickReply, setShowQuickReply] = useState(false)
  const [quickReplyFilter, setQuickReplyFilter] = useState('')
  const [quickRepliesData, setQuickRepliesData] = useState<any[]>([])

  // Typing indicator
  const [contactTyping, setContactTyping] = useState<string | null>(null) // null | 'composing' | 'recording'
  const typingTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const lastTypingSentRef = useRef<number>(0)
  const typingPauseTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // History sync
  const [syncingHistory, setSyncingHistory] = useState(false)

  const cacheMessages = useCallback((targetChatId: string | null | undefined, nextMessages: Message[], nextHasMore = hasMoreMessages) => {
    if (!targetChatId) return
    messagesCacheRef.current.set(targetChatId, {
      messages: nextMessages,
      hasMore: nextHasMore
    })
  }, [hasMoreMessages])

  const updateMessages = useCallback((updater: Message[] | ((prev: Message[]) => Message[]), targetChatId = chatId) => {
    setMessages(prev => {
      const nextMessages = typeof updater === 'function' ? updater(prev) : updater
      cacheMessages(targetChatId, nextMessages)
      return nextMessages
    })
  }, [cacheMessages, chatId])

  const messagesContainerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<WhatsAppTextInputHandle>(null)
  const captionInputRef = useRef<WhatsAppTextInputHandle>(null)
  const optimisticIdRef = useRef(0)
  const previousChatIdRef = useRef<string | null>(chatId)
  const pendingMediaRef = useRef<typeof pendingMedia>(pendingMedia)

  useEffect(() => {
    pendingMediaRef.current = pendingMedia
  }, [pendingMedia])

  // Helper: send typing/composing presence to recipient
  const sendPresence = useCallback((composing: boolean, media: string = '') => {
    if (!chat || !deviceId) return
    const token = localStorage.getItem('token')
    fetch('/api/messages/typing', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ device_id: deviceId, to: chat.jid, composing, media })
    }).catch(() => {})
  }, [chat, deviceId])

  useEffect(() => {
    if (previousChatIdRef.current === chatId) return
    previousChatIdRef.current = chatId

    if (typingPauseTimeoutRef.current) clearTimeout(typingPauseTimeoutRef.current)
    if (pendingMediaRef.current?.previewUrl) {
      URL.revokeObjectURL(pendingMediaRef.current.previewUrl)
    }

    sendPresence(false)
    lastTypingSentRef.current = 0
    setMessageText('')
    setReplyingTo(null)
    setEditingMsg(null)
    setShowQuickReply(false)
    setQuickReplyFilter('')
    setActivePopup(null)
    setShowAttachments(false)
    setPendingMedia(null)
    setMediaCaption('')
    inputRef.current?.clear()
    captionInputRef.current?.clear()
  }, [chatId, sendPresence])

  // Request history sync for current chat
  const handleRequestHistorySync = useCallback(async () => {
    if (!chatId || syncingHistory) return
    setSyncingHistory(true)
    try {
      const token = localStorage.getItem('token')
      const res = await fetch(`/api/chats/${chatId}/sync-history`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` }
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || 'Error solicitando historial')
      }
    } catch (err: any) {
      console.error('[HistorySync]', err)
    } finally {
      // Keep spinning for a bit — response comes async via WebSocket
      setTimeout(() => setSyncingHistory(false), 15000)
    }
  }, [chatId, syncingHistory])

  // Resize
  const [rightPanelWidth, setRightPanelWidth] = useState(320)

  // Fetch quick replies
  useEffect(() => {
    const token = localStorage.getItem('token')
    fetch('/api/quick-replies', { headers: { Authorization: `Bearer ${token}` } })
      .then(res => res.json())
      .then(data => {
        if (data.success) {
          setQuickRepliesData(data.quick_replies || [])
        }
      })
      .catch(console.error)
  }, [])

  // Media query for responsive layout
  const [isMdScreen, setIsMdScreen] = useState(true)
  useEffect(() => {
    const handler = (e: MediaQueryListEvent) => setIsMdScreen(e.matches)
    const mql = window.matchMedia('(min-width: 768px)')
    setIsMdScreen(mql.matches)
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [])

  // Resize handler
  const startResizing = useCallback((mouseDownEvent: React.MouseEvent) => {
    mouseDownEvent.preventDefault();
    const startX = mouseDownEvent.clientX;
    const startWidth = rightPanelWidth;

    const doDrag = (mouseMoveEvent: MouseEvent) => {
      const newWidth = startWidth + (startX - mouseMoveEvent.clientX);
      if (newWidth > 200 && newWidth < 600) {
        setRightPanelWidth(newWidth);
      }
    };

    const stopDrag = () => {
      document.removeEventListener('mousemove', doDrag);
      document.removeEventListener('mouseup', stopDrag);
      document.body.style.cursor = 'default';
      document.body.style.userSelect = '';
    };

    document.addEventListener('mousemove', doDrag);
    document.addEventListener('mouseup', stopDrag);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, [rightPanelWidth]);


  useEffect(() => {
    if (initialChat) {
        setChat(initialChat)
        const cached = messagesCacheRef.current.get(initialChat.id)
        if (cached) {
          setMessages(cached.messages)
          setHasMoreMessages(cached.hasMore)
        } else {
          setMessages([])
          setHasMoreMessages(true)
        }
    }
  }, [initialChat])

  useEffect(() => {
    if (chatId) {
      const cached = messagesCacheRef.current.get(chatId)
      if (cached) {
        setMessages(cached.messages)
        setHasMoreMessages(cached.hasMore)
        requestAnimationFrame(scrollToBottom)
      }
      fetchChatDetails()
    } else {
        setChat(null)
        setMessages([])
    }
  }, [chatId])

  useEffect(() => {
    if (!chatId || !deviceId) return

    const unsubscribe = subscribeWebSocket(
      (data: unknown) => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const msg = data as any
        const eventType = msg.type || msg.event
        const payload = msg.data || msg.message

        if ((eventType === 'new_message' || eventType === 'message_sent') && payload) {
          // The actual message object is nested inside payload.message
          const actualMsg = payload.message || payload
          const matchChatId = payload.chat_id || actualMsg.chat_id
          if (matchChatId === chatId ||
              (chat && actualMsg.from_jid === chat?.jid) ||
              (chat && actualMsg.to === chat?.jid)) {
            updateMessages(prev => {
              // Already in list by real ID → update in place (no duplicate)
              if (prev.some(m => m.id === actualMsg.id)) {
                return prev.map(m => m.id === actualMsg.id ? (actualMsg as Message) : m)
              }
              // Outgoing message (is_from_me) → replace the most recent optimistic
              // message that is still in 'sending' state to avoid duplicates
              if (actualMsg.is_from_me) {
                const sendingIdx = [...prev].reverse().findIndex(m => m.status === 'sending' && m.is_from_me)
                if (sendingIdx !== -1) {
                  const realIdx = prev.length - 1 - sendingIdx
                  const next = [...prev]
                  next[realIdx] = actualMsg as Message
                  return next
                }
                // No optimistic message pending → safe to add (e.g. sent from another device)
                return [...prev, actualMsg as Message]
              }
              // Incoming message → always add
              return [...prev, actualMsg as Message]
            })
            scrollToBottom()
          }
        } else if ((eventType === 'message_update') && payload) {
          const actualMsg = payload.message || payload
          updateMessages(prev => prev.map(m => m.id === actualMsg.id ? (actualMsg as Message) : m))
        } else if (eventType === 'message_status' && payload) {
          // Update message delivery/read status (only upgrade, never downgrade)
          const msgIds: string[] = payload.message_ids || []
          const newStatus: string = payload.status
          const statusOrder: Record<string, number> = { sending: 0, sent: 1, delivered: 2, read: 3 }
          const newLevel = statusOrder[newStatus] ?? -1
          if (chat && payload.chat_jid === chat.jid && msgIds.length > 0 && newLevel >= 0) {
            updateMessages(prev => prev.map(m => {
              if (!msgIds.includes(m.message_id)) return m
              const currentLevel = statusOrder[m.status] ?? -1
              if (newLevel > currentLevel) return { ...m, status: newStatus }
              return m
            }))
          }
        } else if (eventType === 'message_revoked' && payload) {
          // Mark message as revoked
          const revokedMsgId: string = payload.message_id
          if (chat && payload.chat_jid === chat.jid) {
            updateMessages(prev => prev.map(m =>
              m.message_id === revokedMsgId ? { ...m, is_revoked: true, body: undefined } : m
            ))
          }
        } else if (eventType === 'message_edited' && payload) {
          // Update edited message body
          const editedMsgId: string = payload.message_id
          const newBody: string = payload.new_body
          if (chat && payload.chat_jid === chat.jid) {
            updateMessages(prev => prev.map(m =>
              m.message_id === editedMsgId ? { ...m, body: newBody, is_edited: true } : m
            ))
          }
        } else if ((eventType === 'typing' || eventType === 'presence') && payload) {
          // Typing/presence indicator from contact
          if (chat && payload.jid === chat.jid) {
            if (payload.composing || payload.available) {
              const media = payload.media === 'audio' ? 'recording' : 'composing'
              setContactTyping(media)
              // Auto-clear typing after 15s (in case stop event is missed)
              if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
              typingTimeoutRef.current = setTimeout(() => setContactTyping(null), 15000)
            } else {
              setContactTyping(null)
              if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
            }
          }
        } else if (eventType === 'history_sync_complete' && payload) {
          // History sync completed — reload messages to include historical ones
          setSyncingHistory(false)
          fetchChatDetails()
        } else if (eventType === 'message_reaction' && payload) {
          // Incoming reaction from contact or self echo
          if (chat && payload.chat_id === chat.id) {
            const targetMsgId: string = payload.target_message_id
            const emoji: string = payload.emoji
            const senderJid: string = payload.sender_jid || ''
            const senderName: string = payload.sender_name || ''
            const isFromMe: boolean = !!payload.is_from_me
            const removed: boolean = !!payload.removed

            updateMessages(prev => prev.map(m => {
              if (m.message_id !== targetMsgId) return m
              const reactions = [...(m.reactions || [])]
              if (removed) {
                // Remove reaction from this sender
                const idx = reactions.findIndex(r => r.sender_jid === senderJid)
                if (idx >= 0) reactions.splice(idx, 1)
              } else {
                // Upsert: remove previous reaction from same sender, add new
                const idx = reactions.findIndex(r => r.sender_jid === senderJid)
                if (idx >= 0) reactions.splice(idx, 1)
                reactions.push({
                  id: '',
                  target_message_id: targetMsgId,
                  sender_jid: senderJid,
                  sender_name: senderName,
                  emoji,
                  is_from_me: isFromMe
                })
              }
              return { ...m, reactions }
            }))
          }
        }
      },
      (send) => {
        send(JSON.stringify({
          event: 'subscribe_chat',
          data: { chat_id: chatId, device_id: deviceId }
        }))
      }
    )

    return () => {
      unsubscribe()
    }
  }, [chatId, deviceId, chat])

  const fetchChatDetails = async () => {
    if (!chatId) return
    const hasCachedMessages = messagesCacheRef.current.has(chatId)
    setLoading(!hasCachedMessages)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/chats/${chatId}`, {
        headers: { Authorization: `Bearer ${token}` }
      })
      const data = await res.json()
      if (data.success) {
        setChat(data.chat)
      }

      // Fetch messages from dedicated endpoint
      const msgRes = await fetch(`/api/chats/${chatId}/messages?limit=50`, {
        headers: { Authorization: `Bearer ${token}` }
      })
      const msgData = await msgRes.json()
      if (msgData.success && msgData.messages) {
        const nextHasMore = msgData.messages.length >= 50
        setMessages(msgData.messages)
        setHasMoreMessages(nextHasMore)
        cacheMessages(chatId, msgData.messages, nextHasMore)
        scrollToBottom()

        // Send read receipts for unread incoming messages
        if (deviceId && data.chat?.jid) {
          const unreadIncoming = (msgData.messages as Message[]).filter(
            (m: Message) => !m.is_from_me && !m.is_read
          )
          if (unreadIncoming.length > 0) {
            const lastMsg = unreadIncoming[unreadIncoming.length - 1]
            fetch('/api/messages/read-receipt', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
              body: JSON.stringify({
                device_id: deviceId,
                chat_jid: data.chat.jid,
                sender_jid: lastMsg.from_jid || '',
                message_ids: unreadIncoming.map((m: Message) => m.message_id)
              })
            }).catch(() => {})
          }
        }
      }
    } catch (error) {
      console.error('Failed to fetch chat', error)
    } finally {
      setLoading(false)
    }
  }

  const scrollToBottom = () => {
    setTimeout(() => {
      if (messagesContainerRef.current) {
        messagesContainerRef.current.scrollTop = messagesContainerRef.current.scrollHeight
      }
    }, 100)
  }

  const loadOlderMessages = async () => {
    if (loadingMore || !hasMoreMessages || !chatId) return
    setLoadingMore(true)
    const token = localStorage.getItem('token')
    try {
      const offset = messages.length
      const res = await fetch(`/api/chats/${chatId}/messages?limit=50&offset=${offset}`, {
        headers: { Authorization: `Bearer ${token}` }
      })
      const data = await res.json()
      if (data.success && data.messages) {
        if (data.messages.length === 0) {
          setHasMoreMessages(false)
        } else {
          // Preserve scroll position
          const container = messagesContainerRef.current
          const prevHeight = container?.scrollHeight || 0
          const nextHasMore = data.messages.length >= 50
          updateMessages(prev => {
            const nextMessages = [...data.messages, ...prev]
            cacheMessages(chatId, nextMessages, nextHasMore)
            return nextMessages
          })
          setHasMoreMessages(nextHasMore)
          // Restore scroll position after prepending
          requestAnimationFrame(() => {
            if (container) {
              container.scrollTop = container.scrollHeight - prevHeight
            }
          })
        }
      }
    } catch (err) {
      console.error('Failed to load older messages', err)
    } finally {
      setLoadingMore(false)
    }
  }

  const handleMessagesScroll = () => {
    const container = messagesContainerRef.current
    if (!container) return
    if (container.scrollTop < 80 && hasMoreMessages && !loadingMore) {
      loadOlderMessages()
    }
  }

  const handleSendMessage = async () => {
    if ((!messageText.trim() && !forwardingMsg) || !chat || !deviceId) return

    const text = messageText.trim()

    // Handle edit mode
    if (editingMsg) {
      if (!text) return
      const token = localStorage.getItem('token')
      try {
        const res = await fetch('/api/messages/edit', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({
            device_id: deviceId,
            chat_jid: chat.jid,
            message_id: editingMsg.message_id,
            new_body: text
          })
        })
        const data = await res.json()
        if (data.success) {
          updateMessages(prev => prev.map(m =>
            m.message_id === editingMsg.message_id ? { ...m, body: text, is_edited: true } : m
          ))
        } else {
          alert(data.error || 'Error al editar mensaje')
        }
      } catch (err) {
        console.error('Failed to edit message', err)
      }
      setEditingMsg(null)
      setMessageText('')
      inputRef.current?.clear()
      requestAnimationFrame(() => inputRef.current?.focus())
      return
    }

    // Stop typing indicator on send
    if (typingPauseTimeoutRef.current) clearTimeout(typingPauseTimeoutRef.current)
    sendPresence(false)
    lastTypingSentRef.current = 0

    setSendingMessage(true)
    setMessageText('')
    setReplyingTo(null)
    setQuickReplyFilter('')

    if (inputRef.current) {
        inputRef.current.clear()
        // Use rAF to ensure focus happens after React re-render
        requestAnimationFrame(() => inputRef.current?.focus())
    }

    // Optimistic UI
    const tempId = `optimistic-${++optimisticIdRef.current}`
    const optimisticMsg: Message = {
      id: tempId,
      message_id: tempId,
      from_jid: '',
      from_name: 'Me',
      body: text,
      message_type: 'text',
      is_from_me: true,
      is_read: false,
      status: 'sending',
      timestamp: new Date().toISOString(),
      quoted_message_id: replyingTo?.id,
      quoted_body: replyingTo?.body,
      quoted_sender: replyingTo?.from_jid
    }

    updateMessages(prev => [...prev, optimisticMsg])
    scrollToBottom()

    const token = localStorage.getItem('token')
    try {
        const res = await fetch('/api/messages/send', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                Authorization: `Bearer ${token}`
            },
            body: JSON.stringify({
                device_id: deviceId,
                to: chat.jid,
                body: text,
                quoted_message_id: replyingTo?.message_id
            })
        })
        const data = await res.json()

        if (data.success) {
            // Always update the optimistic message from the API response
            const realMsg = data.message
            if (realMsg) {
                updateMessages(prev => prev.map(m => m.id === tempId ? { ...realMsg, is_from_me: true } : m))
            } else {
                updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'sent' } : m))
            }
        } else {
            updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
            // alert('Error al enviar mensaje: ' + (data.error || 'Desconocido'))
        }
    } catch (err) {
        updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
        console.error(err)
    } finally {
        setSendingMessage(false)
    }
  }

  const handleRetrySend = async (failedMsg: Message) => {
    if (!chat || !deviceId) return

    updateMessages(prev => prev.map(m => m.id === failedMsg.id ? { ...m, status: 'sending' } : m))

    const token = localStorage.getItem('token')
    try {
        const res = await fetch('/api/messages/send', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                Authorization: `Bearer ${token}`
            },
            body: JSON.stringify({
                device_id: deviceId,
                to: chat.jid,
                body: failedMsg.body
            })
        })
        const data = await res.json()
        if (data.success && data.message) {
            updateMessages(prev => prev.map(m => m.id === failedMsg.id ? data.message : m))
        } else {
            updateMessages(prev => prev.map(m => m.id === failedMsg.id ? { ...m, status: 'failed' } : m))
        }
    } catch (e) {
        updateMessages(prev => prev.map(m => m.id === failedMsg.id ? { ...m, status: 'failed' } : m))
    }
  }

  const handleSendMedia = async (file: File, mediaType: string, caption?: string) => {
      if (!chat || !deviceId) return

      // Compress images client-side (like WhatsApp: max 1600px, JPEG 70%)
      let fileToSend = file
      if (mediaType === 'image') {
        try {
          fileToSend = await compressImageStandard(file)
        } catch (err) {
          console.warn('[ImageCompress] Compression failed, using original:', err)
        }
      }

      const tempId = `optimistic-${++optimisticIdRef.current}`
      const previewUrl = URL.createObjectURL(fileToSend)
      const finalCaption = caption ?? (mediaType === 'document' ? fileToSend.name : '')

      const optimisticMsg: Message = {
        id: tempId,
        message_id: tempId,
        from_jid: '',
        from_name: 'Me',
        body: finalCaption,
        message_type: mediaType,
        media_url: previewUrl,
        is_from_me: true,
        is_read: false,
        status: 'sending',
        timestamp: new Date().toISOString()
      }

      updateMessages(prev => [...prev, optimisticMsg])
      setActivePopup(null)
      scrollToBottom()

      const token = localStorage.getItem('token')
      try {
           const formData = new FormData()
           formData.append('file', fileToSend)
           formData.append('folder', 'uploads')

           const uploadRes = await fetch('/api/media/upload', {
               method: 'POST',
               headers: { Authorization: `Bearer ${token}` },
               body: formData
           })
           const uploadData = await uploadRes.json()

           if (!uploadData.success) throw new Error(uploadData.error || 'Error al subir archivo')

           const res = await fetch('/api/messages/send', {
             method: 'POST',
             headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
             body: JSON.stringify({
               device_id: deviceId,
               to: chat.jid,
               body: finalCaption,
               media_url: uploadData.proxy_url || uploadData.public_url,
               media_type: mediaType
             })
           })

           const data = await res.json()
           if (data.success) {
               const realMsg = data.message
               if (realMsg) {
                   updateMessages(prev => prev.map(m => m.id === tempId ? { ...realMsg, is_from_me: true } : m))
               } else {
                   updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'sent' } : m))
               }
           } else {
               updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
           }
      } catch (err) {
           console.error(err)
           updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
      }
  }

  const handleSendMediaUrl = async (url: string, mediaType: string, caption: string) => {
    if (!chat || !deviceId) return

    const tempId = `optimistic-${++optimisticIdRef.current}`

    const optimisticMsg: Message = {
      id: tempId,
      message_id: tempId,
      from_jid: '',
      from_name: 'Me',
      body: caption,
      message_type: mediaType,
      media_url: url,
      is_from_me: true,
      is_read: false,
      status: 'sending',
      timestamp: new Date().toISOString()
    }

    updateMessages(prev => [...prev, optimisticMsg])
    scrollToBottom()

    const token = localStorage.getItem('token')
    try {
        const res = await fetch('/api/messages/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
            body: JSON.stringify({
                device_id: deviceId,
                to: chat.jid,
                body: caption,
                media_url: url,
                media_type: mediaType
            })
        })

        const data = await res.json()
        if (data.success) {
            const realMsg = data.message
            if (realMsg) {
                updateMessages(prev => prev.map(m => m.id === tempId ? { ...realMsg, is_from_me: true } : m))
            } else {
                updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'sent' } : m))
            }
        } else {
            updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
        }
    } catch (err) {
        console.error(err)
        updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
    }
  }

  const handleSendSticker = async (stickerUrl: string, file?: File) => {
      if (!chat || !deviceId) return

      if (file) {
          await handleSendMedia(file, 'sticker')
          return
      }

      const tempId = `optimistic-${++optimisticIdRef.current}`
      const optimisticMsg: Message = {
          id: tempId,
          message_id: tempId,
          from_jid: '',
          from_name: 'Me',
          body: '',
          message_type: 'sticker',
          media_url: stickerUrl,
          is_from_me: true,
          is_read: false,
          status: 'sending',
          timestamp: new Date().toISOString()
      }
      updateMessages(prev => [...prev, optimisticMsg])
      scrollToBottom()

      const token = localStorage.getItem('token')
      fetch('/api/messages/send', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({
              device_id: deviceId,
              to: chat.jid,
              media_url: stickerUrl,
              media_type: 'sticker'
          })
      }).then(res => res.json()).then(data => {
          if (data.success) {
              const realMsg = data.message
              if (realMsg) {
                  updateMessages(prev => prev.map(m => m.id === tempId ? { ...realMsg, is_from_me: true } : m))
              } else {
                  updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'sent' } : m))
              }
          } else {
              updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
          }
      })
  }

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>, pickerType: 'media' | 'document') => {
     if (e.target.files && e.target.files.length > 0) {
         const file = e.target.files[0]
         const type = pickerType === 'document'
           ? 'document'
           : file.type.startsWith('video/')
             ? 'video'
             : 'image'

         // Video size limit: 15 MB (like WhatsApp)
         if (type === 'video' && file.size > 15 * 1024 * 1024) {
           alert('El video es demasiado grande. Máximo 15 MB.')
           if (e.target) e.target.value = ''
           return
         }

         const previewUrl = type !== 'document' ? URL.createObjectURL(file) : ''
         setPendingMedia({ file, type, previewUrl })
         setMediaCaption('')
         captionInputRef.current?.clear()
         setShowAttachments(false)
         setTimeout(() => captionInputRef.current?.focus(), 100)
     }
     // Reset input so same file can be selected again
     if (e.target) e.target.value = ''
  }

  const handleSendContact = async (contacts: SelectedPerson[]) => {
    if (!chat || !deviceId) return
    setShowContactPicker(false)
    setShowAttachments(false)
    const token = localStorage.getItem('token')
    for (const contact of contacts) {
      const tempId = `optimistic-${++optimisticIdRef.current}`
      const displayName = contact.name || contact.phone || 'Contacto'
      const optimisticMsg: Message = {
        id: tempId,
        message_id: tempId,
        from_jid: '',
        from_name: 'Me',
        body: displayName,
        message_type: 'contact',
        is_from_me: true,
        is_read: false,
        status: 'sending',
        timestamp: new Date().toISOString(),
        contact_name: displayName,
        contact_phone: contact.phone,
      }
      updateMessages(prev => [...prev, optimisticMsg])
      scrollToBottom()
      try {
        const res = await fetch('/api/messages/send-contact', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({
            device_id: deviceId,
            to: chat.jid,
            contact_name: displayName,
            contact_phone: contact.phone,
          })
        })
        const data = await res.json()
        if (data.success && data.message) {
          updateMessages(prev => prev.map(m => m.id === tempId ? { ...data.message, is_from_me: true } : m))
        } else {
          updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
        }
      } catch {
        updateMessages(prev => prev.map(m => m.id === tempId ? { ...m, status: 'failed' } : m))
      }
    }
  }

  const handleSendPendingMedia = () => {
    if (!pendingMedia) return
    handleSendMedia(pendingMedia.file, pendingMedia.type, mediaCaption.trim())
    URL.revokeObjectURL(pendingMedia.previewUrl)
    setPendingMedia(null)
    setMediaCaption('')
    captionInputRef.current?.clear()
  }

  const handleCancelPendingMedia = () => {
    if (pendingMedia) {
      URL.revokeObjectURL(pendingMedia.previewUrl)
      setPendingMedia(null)
      setMediaCaption('')
      captionInputRef.current?.clear()
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
       e.preventDefault()
       handleSendMessage()
    }
  }

  const handleMessageChange = (text: string) => {
     setMessageText(text)
     if (text.endsWith('/')) {
         setQuickReplyFilter('')
         setShowQuickReply(true)
     } else if (showQuickReply) {
         const match = text.match(/\/(\w*)$/)
         if (match) {
             setQuickReplyFilter(match[1])
         } else {
             setShowQuickReply(false)
         }
     }

     // Send typing indicator (debounced - max once every 3 seconds)
     if (chat && deviceId && text.length > 0) {
       const now = Date.now()
       if (now - lastTypingSentRef.current > 3000) {
         lastTypingSentRef.current = now
         sendPresence(true)
       }
       // Auto-send paused after 5s of no typing
       if (typingPauseTimeoutRef.current) clearTimeout(typingPauseTimeoutRef.current)
       typingPauseTimeoutRef.current = setTimeout(() => {
         sendPresence(false)
         lastTypingSentRef.current = 0
       }, 5000)
     } else if (text.length === 0) {
       // Cleared input — stop composing immediately
       if (typingPauseTimeoutRef.current) clearTimeout(typingPauseTimeoutRef.current)
       sendPresence(false)
       lastTypingSentRef.current = 0
     }
  }

  const handleQuickReplySelect = (reply: any) => {
     const textBeforeCommand = messageText.replace(/\/[\w-]*$/, '')

     // Multi-attachment support
     if (reply.attachments && reply.attachments.length > 0) {
         for (const att of reply.attachments) {
             handleSendMediaUrl(att.media_url, att.media_type || 'image', att.caption || '')
         }
         if (reply.body) {
             // Send body as separate text message
             const sendText = async () => {
                 const token = localStorage.getItem('token')
                 if (!chat || !deviceId) return
                 try {
                     await fetch('/api/messages/send', {
                         method: 'POST',
                         headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
                         body: JSON.stringify({ device_id: deviceId, to: chat.jid, body: reply.body })
                     })
                 } catch {}
             }
             sendText()
         }
         setMessageText(textBeforeCommand.trim())
     } else if (reply.media_url) {
         handleSendMediaUrl(reply.media_url, reply.media_type || 'image', reply.body || '')
         setMessageText(textBeforeCommand.trim())
     } else {
         setMessageText((textBeforeCommand + reply.body).trim())
     }

     setShowQuickReply(false)
     if (inputRef.current) inputRef.current.focus()
  }

  const handleSaveSticker = (url: string) => {
      const saved = JSON.parse(localStorage.getItem('saved_stickers') || '[]')
      if (!saved.includes(url)) {
          saved.push(url)
          localStorage.setItem('saved_stickers', JSON.stringify(saved))
          alert('Sticker guardado')
      }
  }

  const savedStickerUrls = typeof window !== 'undefined' ? JSON.parse(localStorage.getItem('saved_stickers') || '[]') : []

  if (!chat && loading) {
       return (
        <div className={`flex items-center justify-center bg-slate-50 h-full ${className}`}>
           <div className="animate-spin rounded-full h-8 w-8 border-2 border-emerald-200 border-t-emerald-600" />
        </div>
       )
  }

  if (!chat) {
      return (
          <div className={`flex items-center justify-center bg-slate-50 h-full ${className}`}>
             <p className="text-slate-500">Chat no encontrado</p>
          </div>
      )
  }

  return (
    <div className={`flex-1 flex flex-col min-h-0 overflow-hidden h-full ${className}`}>
         {/* Chat header */}
         <div className="h-14 px-4 flex items-center justify-between border-b border-slate-200 bg-white shrink-0">
              <div className="flex items-center gap-3">
                {onClose && (
                  <button
                    onClick={onClose}
                    className="p-1.5 hover:bg-slate-200 rounded-lg"
                  >
                    <ArrowLeft className="w-5 h-5" />
                  </button>
                )}
                <div
                  className="flex items-center gap-3 cursor-pointer"
                  onClick={() => setShowContactInfo(!showContactInfo)}
                >
                    {chat.contact_avatar_url ? (
                        <img src={chat.contact_avatar_url} className="w-9 h-9 rounded-full object-cover" alt="" />
                    ) : (
                        <div className="w-9 h-9 rounded-full bg-slate-200 flex items-center justify-center">
                            <User className="w-5 h-5 text-slate-500" />
                        </div>
                    )}
                    <div>
                        <h3 className="font-semibold text-sm text-slate-900 leading-tight">
                            {getChatDisplayName(chat)}
                        </h3>
                        {contactTyping ? (
                          <p className="text-xs text-emerald-600 font-medium">
                            {contactTyping === 'recording' ? (
                              <span className="flex items-center gap-1">
                                <Mic className="w-3 h-3" />
                                grabando audio...
                              </span>
                            ) : (
                              <span className="flex items-center gap-1">
                                escribiendo
                                <span className="inline-flex">
                                  <span className="animate-bounce" style={{ animationDelay: '0ms' }}>.</span>
                                  <span className="animate-bounce" style={{ animationDelay: '150ms' }}>.</span>
                                  <span className="animate-bounce" style={{ animationDelay: '300ms' }}>.</span>
                                </span>
                              </span>
                            )}
                          </p>
                        ) : (
                          <p className="text-xs text-slate-500">
                               Click para info
                          </p>
                        )}
                    </div>
                </div>
              </div>

              <div className="flex items-center gap-1">
                   <button
                     onClick={handleRequestHistorySync}
                     disabled={syncingHistory}
                     className="p-2 text-slate-500 hover:bg-slate-100 rounded-full transition disabled:opacity-50"
                     title="Sincronizar historial de mensajes"
                   >
                       <RefreshCw className={`w-5 h-5 ${syncingHistory ? 'animate-spin' : ''}`} />
                   </button>
                   <button onClick={() => setShowSearch(!showSearch)} className="p-2 text-slate-500 hover:bg-slate-100 rounded-full transition">
                       <Search className="w-5 h-5" />
                   </button>
                   <button className="p-2 text-slate-500 hover:bg-slate-100 rounded-full transition">
                       <MoreVertical className="w-5 h-5" />
                   </button>
              </div>
         </div>

         {/* Content Area */}
         <div className="flex-1 flex min-h-0 relative">
             {/* Messages */}
             <div
                ref={messagesContainerRef}
                onScroll={handleMessagesScroll}
                className="flex-1 overflow-y-auto bg-[#efeae2] p-4 space-y-2 relative"
                style={{ backgroundImage: 'url("https://user-images.githubusercontent.com/15075759/28719144-86dc0f70-73b1-11e7-911d-60d70fcded21.png")', backgroundRepeat: 'repeat' }}
             >
                  {loadingMore && (
                    <div className="flex justify-center py-3">
                      <div className="animate-spin rounded-full h-5 w-5 border-2 border-emerald-200 border-t-emerald-600" />
                    </div>
                  )}
                  {!hasMoreMessages && messages.length > 0 && (
                    <div className="flex justify-center py-2">
                      <span className="text-xs text-slate-400 bg-white/80 px-3 py-1 rounded-full">Inicio de la conversación</span>
                    </div>
                  )}
                  {messages.map((msg, idx) => {
                      // Date separator between different days
                      let showDateSep = false
                      const msgDate = new Date(msg.timestamp)
                      const isValidDate = msg.timestamp && !isNaN(msgDate.getTime())
                      if (isValidDate) {
                        if (idx === 0) {
                          showDateSep = true
                        } else {
                          const prevDate = new Date(messages[idx - 1].timestamp)
                          if (!isNaN(prevDate.getTime()) && msgDate.toDateString() !== prevDate.toDateString()) {
                            showDateSep = true
                          }
                        }
                      }

                      const contactName = chat ? getChatDisplayName(chat) : undefined

                      return (
                          <div key={msg.id}>
                              {showDateSep && isValidDate && (
                                <div className="flex justify-center my-3">
                                  <span className="bg-white/90 text-slate-600 text-xs px-3 py-1 rounded-lg shadow-sm font-medium">
                                    {format(msgDate, "d 'de' MMMM, yyyy", { locale: es })}
                                  </span>
                                </div>
                              )}
                              <MessageBubble
                                message={msg}
                                contactName={contactName}
                                onMediaClick={(url) => setViewImage(url)}
                                onRetry={() => handleRetrySend(msg)}
                                onReply={(m) => setReplyingTo(m)}
                                onForward={(m) => setForwardingMsg(m)}
                                onDelete={async (m) => {
                                  if (!deviceId || !chat) return
                                  const token = localStorage.getItem('token')
                                  try {
                                    const res = await fetch('/api/messages/delete', {
                                      method: 'POST',
                                      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
                                      body: JSON.stringify({
                                        device_id: deviceId,
                                        chat_jid: chat.jid,
                                        sender_jid: m.from_jid || '',
                                        message_id: m.message_id,
                                        is_from_me: m.is_from_me
                                      })
                                    })
                                    const data = await res.json()
                                    if (data.success) {
                                      updateMessages(prev => prev.map(msg =>
                                        msg.message_id === m.message_id ? { ...msg, is_revoked: true, body: undefined } : msg
                                      ))
                                    }
                                  } catch (err) {
                                    console.error('Failed to delete message', err)
                                  }
                                }}
                                onEdit={(m) => {
                                  setEditingMsg(m)
                                  setMessageText(m.body || '')
                                  requestAnimationFrame(() => {
                                    inputRef.current?.focus()
                                  })
                                }}
                                onReact={async (m, emoji) => {
                                  if (!deviceId || !chat) return
                                  const token = localStorage.getItem('token')
                                  try {
                                    // Optimistically update UI
                                    updateMessages(prev => prev.map(msg => {
                                      if (msg.message_id !== m.message_id) return msg
                                      const reactions = [...(msg.reactions || [])]
                                      const existingIdx = reactions.findIndex(r => r.is_from_me && r.emoji === emoji)
                                      if (existingIdx >= 0) {
                                        reactions.splice(existingIdx, 1)
                                      } else {
                                        const prevIdx = reactions.findIndex(r => r.is_from_me)
                                        if (prevIdx >= 0) reactions.splice(prevIdx, 1)
                                        reactions.push({ id: '', target_message_id: m.message_id, sender_jid: '', emoji, is_from_me: true })
                                      }
                                      return { ...msg, reactions }
                                    }))
                                    const res = await fetch('/api/messages/react', {
                                      method: 'POST',
                                      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
                                      body: JSON.stringify({
                                        device_id: deviceId,
                                        to: chat.jid,
                                        target_message_id: m.message_id,
                                        target_from_me: !!m.is_from_me,
                                        emoji: emoji
                                      })
                                    })
                                    const data = await res.json()
                                    if (!data.success) {
                                      console.error('Failed to send reaction:', data.error)
                                    }
                                  } catch (err) {
                                    console.error('Failed to send reaction', err)
                                  }
                                }}
                              />
                          </div>
                      )
                  })}
             </div>

             {/* Right Panel (Contact/Search) - Overlay/Sidebar — only when NOT parent-controlled */}
             {showContactInfo && !onContactInfoToggle && (
                <div
                   className="absolute top-0 right-0 h-full border-l border-slate-200 bg-white z-20 shadow-xl"
                   style={{ width: isMdScreen ? rightPanelWidth : '100%' }}
                >
                     {/* Drag handle for resizing */}
                     {isMdScreen && (
                        <div
                          className="absolute left-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-emerald-400 transition z-30"
                          onMouseDown={startResizing}
                        />
                     )}

                     <ContactPanel
                        chatId={chat.id}
                        isOpen={true}
                        onClose={() => setShowContactInfo(false)}
                        deviceName={deviceId ? 'Dispositivo actual' : undefined}
                     />
                </div>
             )}
         </div>

         {/* Media Preview Overlay */}
         {pendingMedia && (
           <div className="absolute inset-0 z-40 bg-white flex flex-col">
             {/* Close */}
             <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200">
               <button onClick={handleCancelPendingMedia} className="p-2 text-slate-500 hover:text-slate-800 hover:bg-slate-100 rounded-full transition">
                 <X className="w-6 h-6" />
               </button>
               <span className="text-slate-600 text-sm font-medium">
                 {pendingMedia.type === 'image' ? 'Imagen' : pendingMedia.type === 'video' ? 'Video' : 'Documento'}
               </span>
               <div className="w-10" />
             </div>
             {/* Preview */}
             <div className="flex-1 flex items-center justify-center p-4 min-h-0 bg-slate-50">
               {pendingMedia.type === 'image' ? (
                 <img src={pendingMedia.previewUrl} className="max-h-full max-w-full object-contain rounded-lg shadow-md" alt="Preview" />
               ) : pendingMedia.type === 'video' ? (
                 <video src={pendingMedia.previewUrl} className="max-h-full max-w-full rounded-lg shadow-md" controls />
               ) : (
                 <div className="flex flex-col items-center gap-4 p-8 bg-white rounded-2xl shadow-lg border border-slate-200 max-w-sm">
                   <div className="w-20 h-20 bg-blue-100 rounded-2xl flex items-center justify-center">
                     <FileText className="w-10 h-10 text-blue-500" />
                   </div>
                   <div className="text-center">
                     <p className="text-sm font-semibold text-slate-800 break-all">{pendingMedia.file.name}</p>
                     <p className="text-xs text-slate-400 mt-1">{(pendingMedia.file.size / 1024 / 1024).toFixed(2)} MB</p>
                   </div>
                 </div>
               )}
             </div>
             {/* Caption + Send */}
             <div className="px-4 py-3 flex items-center gap-3 border-t border-slate-200 bg-white">
               <EmojiPicker
                 onEmojiSelect={(emoji) => {
                   if (captionInputRef.current) {
                     captionInputRef.current.insertAtCaret(emoji)
                   } else {
                     setMediaCaption(prev => prev + emoji)
                   }
                 }}
                 buttonClassName="p-2 text-slate-500 hover:text-emerald-600 transition"
               />
               <div className="flex-1">
                 <WhatsAppTextInput
                   ref={captionInputRef}
                   value={mediaCaption}
                   onChange={setMediaCaption}
                   placeholder={pendingMedia.type === 'document' ? 'Agregar descripción...' : 'Agregar pie de foto...'}
                   onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSendPendingMedia() } }}
                   singleLine
                 />
               </div>
               <button
                 onClick={handleSendPendingMedia}
                 className="p-3 bg-emerald-600 text-white rounded-full hover:bg-emerald-700 transition shadow-md"
               >
                 <Send className="w-5 h-5" />
               </button>
             </div>
           </div>
         )}

         {/* Footer / Input */}
         {readOnly ? (
           <div className="px-4 py-3 bg-amber-50 border-t border-amber-200 flex items-center justify-center gap-2 shrink-0">
             <EyeOff className="w-4 h-4 text-amber-600" />
             <span className="text-sm text-amber-700 font-medium">Solo lectura — dispositivo no conectado</span>
           </div>
         ) : (
         <div className="px-3 py-2 bg-slate-50 border-t border-slate-200 flex items-end gap-2 relative z-30 shrink-0">
              {editingMsg && (
                  <div className="absolute bottom-full left-0 right-0 bg-blue-50 p-2 border-t border-blue-400 flex justify-between items-center shadow-sm">
                      <div className="text-xs border-l-4 border-blue-500 pl-2">
                          <p className="font-bold text-blue-700">Editando mensaje</p>
                          <p className="line-clamp-1 text-slate-600">{editingMsg.body}</p>
                      </div>
                      <button onClick={() => { setEditingMsg(null); setMessageText(''); inputRef.current?.clear() }}><X className="w-4 h-4 text-slate-500" /></button>
                  </div>
              )}
              {replyingTo && (
                  <div className="absolute bottom-full left-0 right-0 bg-slate-100 p-2 border-t border-emerald-500 flex justify-between items-center shadow-sm">
                      <div className="text-xs border-l-4 border-emerald-500 pl-2">
                          <p className="font-bold text-emerald-700">Respondiendo a {replyingTo.is_from_me ? 'ti mismo' : 'contacto'}</p>
                          <p className="line-clamp-1 text-slate-600">{replyingTo.body || 'Media'}</p>
                      </div>
                      <button onClick={() => setReplyingTo(null)}><X className="w-4 h-4 text-slate-500" /></button>
                  </div>
              )}

              {/* Attachments Menu */}
              {showAttachments && (
                  <div className="absolute bottom-16 left-4 bg-white rounded-xl shadow-xl border border-slate-100 p-2 flex flex-col gap-2 animate-in slide-in-from-bottom-2 duration-200">
                      <button className="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-lg transition text-sm text-slate-700" onClick={() => { fileInputRef.current?.click(); setShowAttachments(false) }}>
                          <ImageIcon className="w-5 h-5 text-purple-500" /> Foto/Video
                      </button>
                      <button className="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-lg transition text-sm text-slate-700" onClick={() => { docFileInputRef.current?.click(); setShowAttachments(false) }}>
                          <FileText className="w-5 h-5 text-blue-500" /> Documento
                      </button>
                      <button className="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-lg transition text-sm text-slate-700" onClick={() => { setShowContactPicker(true); setShowAttachments(false) }}>
                          <User className="w-5 h-5 text-emerald-500" /> Contacto
                      </button>
                  </div>
              )}
              <input type="file" ref={fileInputRef} className="hidden" accept="image/*,video/*" onChange={e => handleFileSelect(e, 'media')} />
              <input type="file" ref={docFileInputRef} className="hidden" accept="image/*,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt,.csv,.zip,.rar" onChange={e => handleFileSelect(e, 'document')} />

              <div className="flex gap-1 pb-1">
                  <button onClick={() => setShowAttachments(!showAttachments)} className="p-2 text-slate-500 hover:text-emerald-600 transition">
                      <Paperclip className="w-6 h-6" />
                  </button>
                  <EmojiPicker
                    onEmojiSelect={(emoji) => setMessageText(prev => prev + emoji)}
                    isOpen={activePopup === 'emoji'}
                    onToggle={() => setActivePopup(activePopup === 'emoji' ? null : 'emoji')}
                    buttonClassName={`p-2 transition ${activePopup === 'emoji' ? 'text-emerald-600' : 'text-slate-500 hover:text-emerald-600'}`}
                  />
                  <StickerPicker
                      onStickerSelect={handleSendSticker}
                      isOpen={activePopup === 'sticker'}
                      onToggle={() => setActivePopup(activePopup === 'sticker' ? null : 'sticker')}
                  />
              </div>

              <div className="flex-1 relative">
                    <QuickReplyPicker
                      replies={quickRepliesData}
                      isOpen={showQuickReply}
                      filter={quickReplyFilter}
                      onSelect={handleQuickReplySelect}
                      onClose={() => { setShowQuickReply(false); setQuickReplyFilter('') }}
                    />
                    <WhatsAppTextInput
                      ref={inputRef}
                      value={messageText}
                      onChange={handleMessageChange}
                      placeholder="Escribe un mensaje... ( / para respuestas rápidas)"
                      onKeyDown={handleKeyDown}
                      singleLine
                    />
              </div>

              {(messageText || forwardingMsg) && (
                  <button
                    onClick={handleSendMessage}
                    disabled={sendingMessage}
                    className="p-3 bg-emerald-600 text-white rounded-full hover:bg-emerald-700 disabled:opacity-50 transition shadow-md"
                  >
                      <Send className="w-5 h-5" />
                  </button>
              )}
         </div>
         )}

         {/* Image Viewer */}
         {viewImage && (
             <ImageViewer src={viewImage} isOpen={!!viewImage} onClose={() => setViewImage(null)} />
         )}

         {/* Forward Modal */}
         {forwardingMsg && chat && deviceId && (
             <ForwardMessageModal
               message={forwardingMsg}
               deviceId={deviceId}
               chatId={chat.id}
               onClose={() => setForwardingMsg(null)}
               onSuccess={() => setForwardingMsg(null)}
             />
         )}

         {/* Contact Picker for sending contact vCard */}
         <ContactSelector
           open={showContactPicker}
           onClose={() => setShowContactPicker(false)}
           onConfirm={handleSendContact}
           title="Enviar Contacto"
           subtitle="Selecciona los contactos que deseas enviar"
           confirmLabel="Enviar"
         />
    </div>
  )
}

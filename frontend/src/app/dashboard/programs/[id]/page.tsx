"use client";

import { useState, useEffect, useMemo, useCallback } from 'react';
import { useParams, useRouter } from 'next/navigation';
import {
  ArrowLeft, Users, Calendar, MessageSquare, Plus, Check, X, Clock,
  AlertCircle, Trash2, GraduationCap, MapPin, CalendarDays, Send,
  Repeat, ChevronRight, CheckCircle2, XCircle, Phone, Edit2, MoreVertical, Archive, BarChart3, Columns3, LayoutGrid
} from 'lucide-react';
import { api } from '@/lib/api';
import { createWhatsAppChat, deviceDisplayPhone, relationClassName, relationLabel, resolveWhatsAppChat, type WhatsAppDeviceOption } from '@/lib/whatsappChatLauncher';
import { Program, ProgramParticipant, ProgramSession, ProgramAttendance } from '@/types/program';
import { Chat } from '@/types/chat';
import { Contact } from '@/types/contact';
import ContactSelector, { SelectedPerson } from '@/components/ContactSelector';
import CreateCampaignModal, { CampaignFormResult } from '@/components/CreateCampaignModal';
import LeadDetailPanel from '@/components/LeadDetailPanel';
import ChatPanel from '@/components/chat/ChatPanel';
import { format } from 'date-fns';
import { es } from 'date-fns/locale';

const token = () => typeof window !== 'undefined' ? localStorage.getItem('token') || '' : '';

interface Device {
  id: string;
  name: string;
  phone: string | null;
  phone_number?: string;
  jid?: string | null;
  status: string;
  normalized_phone?: string;
  historical_relation?: WhatsAppDeviceOption['historical_relation'];
  matches_historical?: boolean;
}

const DAY_NAMES = ['Dom', 'Lun', 'Mar', 'Mié', 'Jue', 'Vie', 'Sáb'];
const DAY_NAMES_FULL = ['Domingo', 'Lunes', 'Martes', 'Miércoles', 'Jueves', 'Viernes', 'Sábado'];

const contactToLead = (c: Contact) => ({
  id: c.id,
  jid: c.jid || '',
  contact_id: c.id,
  name: c.custom_name ?? c.name ?? '',
  last_name: c.last_name ?? null,
  short_name: c.short_name ?? null,
  phone: c.phone ?? '',
  email: c.email ?? '',
  company: c.company ?? null,
  age: c.age ?? null,
  dni: c.dni ?? null,
  birth_date: c.birth_date ?? null,
  address: c.address ?? null,
  distrito: c.distrito ?? null,
  ocupacion: c.ocupacion ?? null,
  status: 'new',
  pipeline_id: null,
  stage_id: null,
  stage_name: null,
  stage_color: null,
  stage_position: null,
  notes: c.notes ?? '',
  tags: c.tags || [],
  structured_tags: c.structured_tags || null,
  kommo_id: c.kommo_id ?? null,
  is_archived: false,
  archived_at: null,
  is_blocked: false,
  blocked_at: null,
  block_reason: '',
  assigned_to: '',
  created_at: c.created_at || '',
  updated_at: c.updated_at || '',
}) as any;

export default function ProgramDetailPage() {
  const params = useParams();
  const router = useRouter();
  const programId = params.id as string;

  const [program, setProgram] = useState<Program | null>(null);
  const [participants, setParticipants] = useState<ProgramParticipant[]>([]);
  const [sessions, setSessions] = useState<ProgramSession[]>([]);
  const [stages, setStages] = useState<Array<{ id: string; name: string; color: string; position: number }>>([]);
  const [draggedParticipantID, setDraggedParticipantID] = useState<string | null>(null);
  const [dragOverStageID, setDragOverStageID] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'participants' | 'sessions' | 'stats' | 'kanban'>('participants');
  const [loading, setLoading] = useState(true);
  const [devices, setDevices] = useState<Device[]>([]);

  // Modals state
  const [isAddParticipantOpen, setIsAddParticipantOpen] = useState(false);
  const [isCreateSessionOpen, setIsCreateSessionOpen] = useState(false);
  const [isAttendanceOpen, setIsAttendanceOpen] = useState(false);
  const [showCampaignModal, setShowCampaignModal] = useState(false);
  const [isGenerateSessionsOpen, setIsGenerateSessionsOpen] = useState(false);

  // Form state
  const [newSession, setNewSession] = useState({ date: new Date().toISOString().split('T')[0], topic: '', start_time: '', end_time: '', location: '' });
  const [selectedSession, setSelectedSession] = useState<ProgramSession | null>(null);
  const [attendanceData, setAttendanceData] = useState<Record<string, { status: string, notes: string }>>({});

  // Edit session state
  const [editingSession, setEditingSession] = useState<ProgramSession | null>(null);
  const [editSessionForm, setEditSessionForm] = useState({ date: '', topic: '', start_time: '', end_time: '', location: '' });
  const [savingSession, setSavingSession] = useState(false);

  // Campaign state
  const [creatingCampaign, setCreatingCampaign] = useState(false);

  // Toast state
  const [toastMessage, setToastMessage] = useState<string | null>(null);
  const [toastType, setToastType] = useState<'success' | 'error'>('success');

  // Confirmation dialog state
  const [confirmAction, setConfirmAction] = useState<{ message: string; onConfirm: () => void } | null>(null);

  // Edit program state
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [editForm, setEditForm] = useState({ name: '', description: '', color: '#10b981', status: 'active' });
  const [saving, setSaving] = useState(false);
  const [showHeaderMenu, setShowHeaderMenu] = useState(false);

  // Participant detail panel
  const [selectedLead, setSelectedLead] = useState<any | null>(null);
  const [loadingLead, setLoadingLead] = useState(false);
  const [selectedContact, setSelectedContact] = useState<Contact | null>(null);
  const [contactPanelMode, setContactPanelMode] = useState(false);
  const [selectedParticipantID, setSelectedParticipantID] = useState<string | null>(null);

  // Column visibility (persisted in localStorage)
  const PARTICIPANT_COLUMNS: { id: string; label: string; always?: boolean }[] = [
    { id: 'name', label: 'Nombre', always: true },
    { id: 'phone', label: 'Teléfono' },
    { id: 'status', label: 'Estado' },
    { id: 'enrolled_at', label: 'Inscripción' },
    { id: 'actions', label: 'Acciones' },
  ];
  const DEFAULT_VISIBLE_COLUMNS = ['name', 'phone', 'status', 'enrolled_at', 'actions'];
  const COLUMN_STORAGE_KEY = 'programParticipantColumns:v1';
  const [visibleColumns, setVisibleColumns] = useState<string[]>(DEFAULT_VISIBLE_COLUMNS);
  const [showColumnPicker, setShowColumnPicker] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    try {
      const raw = localStorage.getItem(COLUMN_STORAGE_KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed) && parsed.length > 0) {
          // Ensure 'name' always present, filter unknown ids
          const valid = parsed.filter((c: any) => typeof c === 'string' && PARTICIPANT_COLUMNS.some(pc => pc.id === c));
          if (!valid.includes('name')) valid.unshift('name');
          setVisibleColumns(valid);
        }
      }
    } catch {
      /* ignore */
    }
  }, []);

  const toggleColumn = (id: string) => {
    const col = PARTICIPANT_COLUMNS.find(c => c.id === id);
    if (col?.always) return;
    setVisibleColumns(prev => {
      const next = prev.includes(id) ? prev.filter(c => c !== id) : [...prev, id];
      // Persist in catalog order
      const ordered = PARTICIPANT_COLUMNS.filter(c => next.includes(c.id)).map(c => c.id);
      try { localStorage.setItem(COLUMN_STORAGE_KEY, JSON.stringify(ordered)); } catch { /* ignore */ }
      return ordered;
    });
  };

  // WhatsApp inline chat
  const [showInlineChat, setShowInlineChat] = useState(false);
  const [inlineChatId, setInlineChatId] = useState('');
  const [inlineChat, setInlineChat] = useState<Chat | null>(null);
  const [inlineChatDeviceId, setInlineChatDeviceId] = useState('');
  const [inlineChatReadOnly, setInlineChatReadOnly] = useState(false);
  const [showDeviceSelector, setShowDeviceSelector] = useState(false);
  const [whatsappPhone, setWhatsappPhone] = useState('');
  const [existingChatForWA, setExistingChatForWA] = useState<Chat | null>(null);
  const [whatsappHistoricalPhone, setWhatsappHistoricalPhone] = useState('');

  // Generate sessions form
  const [genForm, setGenForm] = useState({
    start_date: new Date().toISOString().split('T')[0],
    end_date: '',
    days_of_week: [] as number[],
    start_time: '09:00',
    end_time: '10:00',
    topic_prefix: 'Sesión',
    location: '',
  });
  const [generating, setGenerating] = useState(false);

  // Stats
  const [statsData, setStatsData] = useState<{ session_stats: any[]; participant_stats: any[] } | null>(null);
  const [loadingStats, setLoadingStats] = useState(false);
  const [selectedMonths, setSelectedMonths] = useState<string[]>([]);

  // Toast auto-dismiss
  useEffect(() => {
    if (!toastMessage) return;
    const t = setTimeout(() => setToastMessage(null), 3000);
    return () => clearTimeout(t);
  }, [toastMessage]);

  const showToast = (message: string, type: 'success' | 'error') => {
    setToastMessage(message);
    setToastType(type);
  };

  useEffect(() => {
    if (programId) {
      fetchProgramData();
      fetchDevices();
    }
  }, [programId]);

  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (showDeviceSelector) { setShowDeviceSelector(false); return; }
      if (showInlineChat) { setShowInlineChat(false); return; }
      if (isAttendanceOpen) { setIsAttendanceOpen(false); return; }
      if (isGenerateSessionsOpen) { setIsGenerateSessionsOpen(false); return; }
      if (isCreateSessionOpen) { setIsCreateSessionOpen(false); return; }
      if (editingSession) { setEditingSession(null); return; }
      if (isAddParticipantOpen) { setIsAddParticipantOpen(false); return; }
      if (isEditModalOpen) { setIsEditModalOpen(false); return; }
      if (showCampaignModal) { setShowCampaignModal(false); return; }
      if (selectedLead) { setSelectedLead(null); setSelectedParticipantID(null); return; }
    };
    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [showDeviceSelector, showInlineChat, isAttendanceOpen, isGenerateSessionsOpen, isCreateSessionOpen, editingSession, isAddParticipantOpen, isEditModalOpen, showCampaignModal, selectedLead]);

  const fetchProgramData = async () => {
    try {
      setLoading(true);
      const [progRes, partsRes, sessRes] = await Promise.all([
        api<Program>(`/api/programs/${programId}`),
        api<ProgramParticipant[]>(`/api/programs/${programId}/participants`),
        api<ProgramSession[]>(`/api/programs/${programId}/sessions`)
      ]);

      if (progRes.success) setProgram(progRes.data || null);
      if (partsRes.success) setParticipants(partsRes.data || []);
      if (sessRes.success) setSessions(sessRes.data || []);
      // Load stages if event type
      const prog = progRes.success ? (progRes.data as Program | null) : null;
      if (prog && prog.type === 'event' && prog.pipeline_id) {
        try {
          const pipeRes = await fetch(`/api/events/pipelines/${prog.pipeline_id}`, {
            headers: { Authorization: `Bearer ${token()}` },
          });
          const pipeData = await pipeRes.json();
          if (pipeData.success && pipeData.pipeline?.stages) {
            setStages(pipeData.pipeline.stages.map((s: any) => ({
              id: s.id,
              name: s.name,
              color: s.color,
              position: s.position,
            })));
          }
        } catch (e) { console.error('stages', e); }
        setActiveTab(prev => (prev === 'sessions' || prev === 'stats') ? 'kanban' : prev);
      }
    } catch (error) {
      console.error('Error fetching program data:', error);
    } finally {
      setLoading(false);
    }
  };

  const fetchDevices = async () => {
    try {
      const res = await fetch('/api/devices', { headers: { Authorization: `Bearer ${token()}` } });
      const data = await res.json();
      if (data.success) setDevices((data.devices || []).filter((d: Device) => d.status === 'connected'));
    } catch (e) { console.error(e); }
  };

  const fetchStats = useCallback(async (months?: string[]) => {
    setLoadingStats(true);
    try {
      const params = new URLSearchParams();
      const m = months ?? selectedMonths;
      if (m.length > 0) params.set('months', m.join(','));
      const qs = params.toString();
      const res = await fetch(`/api/programs/${programId}/attendance-stats${qs ? '?' + qs : ''}`, { headers: { Authorization: `Bearer ${token()}` } });
      const data = await res.json();
      if (data.success) setStatsData({ session_stats: data.session_stats || [], participant_stats: data.participant_stats || [] });
    } catch (e) { console.error(e); }
    finally { setLoadingStats(false); }
  }, [programId, selectedMonths]);

  // Fetch stats when tab is activated
  useEffect(() => {
    if (activeTab === 'stats' && !statsData && !loadingStats) fetchStats();
  }, [activeTab, statsData, loadingStats, fetchStats]);

  // Re-fetch stats when month filter changes
  useEffect(() => {
    if (activeTab === 'stats' && statsData) fetchStats(selectedMonths);
  }, [selectedMonths]);

  const handleAddParticipants = async (selected: SelectedPerson[]) => {
    try {
      for (const person of selected) {
        await api(`/api/programs/${programId}/participants`, {
          method: 'POST',
          body: JSON.stringify({
            contact_id: person.id,
            status: 'active'
          })
        });
      }
      setIsAddParticipantOpen(false);
      fetchProgramData();
    } catch (error) {
      console.error('Error adding participants:', error);
    }
  };

  // Edit program
  const openEditModal = () => {
    if (program) {
      setEditForm({
        name: program.name,
        description: program.description || '',
        color: program.color || '#10b981',
        status: program.status,
      });
      setIsEditModalOpen(true);
      setShowHeaderMenu(false);
    }
  };

  const handleUpdateProgram = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!program) return;
    setSaving(true);
    try {
      const res = await api(`/api/programs/${programId}`, {
        method: 'PUT',
        body: JSON.stringify(editForm)
      });
      if (res.success) {
        setIsEditModalOpen(false);
        showToast('Programa actualizado', 'success');
        fetchProgramData();
      } else {
        showToast('Error al actualizar programa', 'error');
      }
    } catch (error) {
      console.error('Error updating program:', error);
      showToast('Error al actualizar programa', 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteProgram = async () => {
    setShowHeaderMenu(false);
    setConfirmAction({
      message: '¿Estás seguro de eliminar este programa? Se eliminarán también todos sus participantes, sesiones y asistencia.',
      onConfirm: async () => {
        setConfirmAction(null);
        try {
          const res = await api(`/api/programs/${programId}`, { method: 'DELETE' });
          if (res.success) {
            showToast('Programa eliminado', 'success');
            router.push('/dashboard/programs');
          } else {
            showToast('Error al eliminar programa', 'error');
          }
        } catch (error) {
          console.error('Error deleting program:', error);
          showToast('Error al eliminar programa', 'error');
        }
      },
    });
  };

  const handleArchiveProgram = async () => {
    if (!program) return;
    const newStatus = program.status === 'archived' ? 'active' : 'archived';
    try {
      const res = await api(`/api/programs/${programId}`, {
        method: 'PUT',
        body: JSON.stringify({ ...program, status: newStatus, description: program.description || '' })
      });
      if (res.success) {
        showToast(newStatus === 'archived' ? 'Programa archivado' : 'Programa desarchivado', 'success');
        fetchProgramData();
      } else {
        showToast('Error al cambiar estado del programa', 'error');
      }
      setShowHeaderMenu(false);
    } catch (error) {
      console.error('Error archiving program:', error);
      showToast('Error al cambiar estado del programa', 'error');
    }
  };

  // Participant detail — programs are contact-only by spec (001-program-contacts-only)
  const handleParticipantClick = async (participant: ProgramParticipant) => {
    setSelectedParticipantID(participant.id);
    if (!participant.contact_id) return;
    setContactPanelMode(true);
    setLoadingLead(true);
    try {
      const res = await fetch(`/api/contacts/${participant.contact_id}`, {
        headers: { Authorization: `Bearer ${token()}` }
      });
      const data = await res.json();
      if (data.success && data.contact) {
        setSelectedContact(data.contact);
        setSelectedLead(contactToLead(data.contact));
      }
    } catch (error) {
      console.error('Error fetching contact:', error);
    } finally {
      setLoadingLead(false);
    }
  };

  // WhatsApp chat
  const handleSendWhatsApp = async (phone: string) => {
    setWhatsappPhone(phone);
    try {
      const resolution = await resolveWhatsAppChat(phone);
      if (!resolution.success) {
        alert(resolution.error || 'Error al resolver conversación');
        return;
      }
      setExistingChatForWA(resolution.chat || null);
      setWhatsappHistoricalPhone(resolution.historical_phone || '');
      if (resolution.mode === 'read_only' && resolution.chat) {
        setInlineChatId(resolution.chat.id);
        setInlineChat(resolution.chat);
        setInlineChatDeviceId(resolution.chat.device_id || '');
        setInlineChatReadOnly(true);
        setShowInlineChat(true);
        return;
      }
      if (resolution.mode === 'open_direct' && resolution.devices[0]) {
        await handleDeviceSelectedForChat(resolution.devices[0] as Device, phone);
        return;
      }
      if (resolution.mode === 'choose_device') {
        setDevices(resolution.devices as Device[]);
        setShowDeviceSelector(true);
        return;
      }
      alert('No hay dispositivos conectados para enviar');
    } catch {
      alert('Error de conexión');
    }
  };

  const handleDeviceSelectedForChat = async (device: Device, phone?: string) => {
    setShowDeviceSelector(false);
    setInlineChatReadOnly(false);
    try {
      const data = await createWhatsAppChat(device.id, phone || whatsappPhone);
      if (data.success && data.chat) {
        setInlineChatId(data.chat.id);
        setInlineChat(data.chat);
        setInlineChatDeviceId(device.id);
        setShowInlineChat(true);
      } else {
        alert(data.error || 'Error al crear conversación');
      }
    } catch {
      alert('Error de conexión');
    }
  };

  const handleRemoveParticipant = async (participantId: string) => {
    setConfirmAction({
      message: '¿Estás seguro de eliminar a este participante?',
      onConfirm: async () => {
        setConfirmAction(null);
        try {
          const res = await api(`/api/programs/${programId}/participants/${participantId}`, { method: 'DELETE' });
          if (res.success) {
            showToast('Participante eliminado', 'success');
            fetchProgramData();
          } else {
            showToast('Error al eliminar participante', 'error');
          }
        } catch (error) {
          console.error('Error removing participant:', error);
          showToast('Error al eliminar participante', 'error');
        }
      },
    });
  };

  const handleCreateSession = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await api(`/api/programs/${programId}/sessions`, {
        method: 'POST',
        body: JSON.stringify({
          ...newSession,
          start_time: newSession.start_time || undefined,
          end_time: newSession.end_time || undefined,
          location: newSession.location || undefined,
        })
      });
      if (res.success) {
        setIsCreateSessionOpen(false);
        setNewSession({ date: new Date().toISOString().split('T')[0], topic: '', start_time: '', end_time: '', location: '' });
        showToast('Sesión creada', 'success');
        fetchProgramData();
      } else {
        showToast('Error al crear sesión', 'error');
      }
    } catch (error) {
      console.error('Error creating session:', error);
      showToast('Error al crear sesión', 'error');
    }
  };

  const openEditSession = (session: ProgramSession) => {
    setEditSessionForm({
      date: session.date ? session.date.split('T')[0] : '',
      topic: session.topic || '',
      start_time: session.start_time || '',
      end_time: session.end_time || '',
      location: session.location || '',
    });
    setEditingSession(session);
  };

  const handleUpdateSession = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingSession) return;
    setSavingSession(true);
    try {
      const res = await api(`/api/programs/${programId}/sessions/${editingSession.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          ...editSessionForm,
          start_time: editSessionForm.start_time || undefined,
          end_time: editSessionForm.end_time || undefined,
          location: editSessionForm.location || undefined,
        })
      });
      if (res.success) {
        setEditingSession(null);
        showToast('Sesión actualizada', 'success');
        fetchProgramData();
      } else {
        showToast('Error al actualizar sesión', 'error');
      }
    } catch (error) {
      console.error('Error updating session:', error);
      showToast('Error al actualizar sesión', 'error');
    } finally {
      setSavingSession(false);
    }
  };

  const handleDeleteSession = async (sessionId: string) => {
    setConfirmAction({
      message: '¿Estás seguro de eliminar esta sesión?',
      onConfirm: async () => {
        setConfirmAction(null);
        try {
          const res = await api(`/api/programs/${programId}/sessions/${sessionId}`, { method: 'DELETE' });
          if (res.success) {
            showToast('Sesión eliminada', 'success');
            fetchProgramData();
          } else {
            showToast('Error al eliminar sesión', 'error');
          }
        } catch (error) {
          console.error('Error deleting session:', error);
          showToast('Error al eliminar sesión', 'error');
        }
      },
    });
  };

  const openAttendance = async (session: ProgramSession) => {
    setSelectedSession(session);
    try {
      const response = await api<ProgramAttendance[]>(`/api/programs/${programId}/sessions/${session.id}/attendance`);
      const attMap: Record<string, { status: string, notes: string }> = {};

      if (response.success && response.data && Array.isArray(response.data)) {
        response.data.forEach((a: ProgramAttendance) => {
          attMap[a.participant_id] = { status: a.status, notes: a.notes || '' };
        });
      }

      setAttendanceData(attMap);
      setIsAttendanceOpen(true);
    } catch (error) {
      console.error('Error fetching attendance:', error);
    }
  };

  const saveAttendance = async () => {
    if (!selectedSession) return;
    try {
      const records = Object.entries(attendanceData)
        .filter(([, data]) => data.status || data.notes?.trim())
        .map(([participantId, data]) => ({
          participant_id: participantId,
          status: data.status || '',
          notes: data.notes || ''
        }));
      const BATCH_SIZE = 100;
      for (let i = 0; i < records.length; i += BATCH_SIZE) {
        const chunk = records.slice(i, i + BATCH_SIZE);
        await api(`/api/programs/${programId}/sessions/${selectedSession.id}/attendance/batch`, {
          method: 'POST',
          body: JSON.stringify({ records: chunk })
        });
      }
      setIsAttendanceOpen(false);
      fetchProgramData();
    } catch (error) {
      console.error('Error saving attendance:', error);
    }
  };

  // Kanban drag/drop handlers
  const handleStageDragStart = (e: React.DragEvent, participantID: string) => {
    setDraggedParticipantID(participantID);
    try { e.dataTransfer.effectAllowed = 'move'; } catch {}
  };
  const handleStageDragOver = (e: React.DragEvent, stageID: string) => {
    e.preventDefault();
    try { e.dataTransfer.dropEffect = 'move'; } catch {}
    if (dragOverStageID !== stageID) setDragOverStageID(stageID);
  };
  const handleStageDragLeave = () => setDragOverStageID(null);
  const handleStageDrop = async (e: React.DragEvent, stageID: string | null) => {
    e.preventDefault();
    const pid = draggedParticipantID;
    setDraggedParticipantID(null);
    setDragOverStageID(null);
    if (!pid) return;
    const participant = participants.find(p => p.id === pid);
    if (!participant) return;
    if ((participant.stage_id || null) === (stageID || null)) return;
    // Optimistic update
    setParticipants(prev => prev.map(p =>
      p.id === pid ? { ...p, stage_id: stageID || undefined, stage_name: stages.find(s => s.id === stageID)?.name, stage_color: stages.find(s => s.id === stageID)?.color } : p
    ));
    try {
      const res = await api(`/api/programs/${programId}/participants/${pid}/stage`, {
        method: 'PATCH',
        body: JSON.stringify({ stage_id: stageID }),
      });
      if (!res.success) {
        showToast((res as any).error || 'Error al mover participante', 'error');
        fetchProgramData();
      }
    } catch (err) {
      console.error(err);
      showToast('Error al mover participante', 'error');
      fetchProgramData();
    }
  };

  // Generate Sessions
  const handleGenerateSessions = async () => {
    if (!genForm.start_date || !genForm.end_date || genForm.days_of_week.length === 0) {
      alert('Completa la fecha de inicio, fin y al menos un día de la semana.');
      return;
    }
    setGenerating(true);
    try {
      const res = await fetch(`/api/programs/${programId}/sessions/generate`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ...genForm,
          location: genForm.location || undefined,
        }),
      });
      const data = await res.json();
      if (data.success) {
        alert(`Se generaron ${data.count} sesiones exitosamente.`);
        setIsGenerateSessionsOpen(false);
        fetchProgramData();
      } else {
        alert(data.error || 'Error al generar sesiones');
      }
    } catch (e) {
      console.error(e);
      alert('Error de conexión');
    } finally {
      setGenerating(false);
    }
  };

  const toggleGenDay = (day: number) => {
    setGenForm(prev => ({
      ...prev,
      days_of_week: prev.days_of_week.includes(day)
        ? prev.days_of_week.filter(d => d !== day)
        : [...prev.days_of_week, day].sort()
    }));
  };

  // Campaign
  const participantsWithPhone = useMemo(
    () => participants.filter(p => p.contact_phone && p.contact_phone.length > 5),
    [participants]
  );

  const handleCreateCampaign = async (formResult: CampaignFormResult) => {
    setCreatingCampaign(true);
    try {
      const res = await fetch(`/api/programs/${programId}/campaign`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: formResult.name,
          device_id: formResult.device_id,
          message_template: formResult.message_template,
          attachments: formResult.attachments,
          scheduled_at: formResult.scheduled_at || undefined,
          settings: formResult.settings,
        }),
      });
      const data = await res.json();
      if (data.success) {
        if (formResult.scheduled_at && data.campaign) {
          await fetch(`/api/campaigns/${data.campaign.id}`, {
            method: 'PUT',
            headers: { Authorization: `Bearer ${token()}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({ status: 'scheduled', scheduled_at: formResult.scheduled_at }),
          });
        }
        // Add spreadsheet recipients if any
        if (formResult.recipients && formResult.recipients.length > 0 && data.campaign) {
          const sheetRecipients = formResult.recipients.map(r => ({
            jid: r.phone + '@s.whatsapp.net',
            name: r.name || '',
            phone: r.phone,
            metadata: r.metadata || {},
          }));
          await fetch(`/api/campaigns/${data.campaign.id}/recipients`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${token()}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({ recipients: sheetRecipients }),
          });
        }
        const extraCount = formResult.recipients?.length || 0;
        alert(`Campaña creada con ${(data.recipients_count || 0) + extraCount} destinatarios. Puedes verla e iniciarla en Envíos Masivos.`);
        setShowCampaignModal(false);
      } else {
        alert(data.error || 'Error al crear campaña');
      }
    } catch (e) {
      console.error(e);
      alert('Error de conexión');
    }
    setCreatingCampaign(false);
  };

  // Preview sessions count
  const previewSessionCount = useMemo(() => {
    if (!genForm.start_date || !genForm.end_date || genForm.days_of_week.length === 0) return 0;
    const start = new Date(genForm.start_date + 'T00:00:00');
    const end = new Date(genForm.end_date + 'T00:00:00');
    if (isNaN(start.getTime()) || isNaN(end.getTime()) || start > end) return 0;
    let count = 0;
    const current = new Date(start);
    while (current <= end) {
      if (genForm.days_of_week.includes(current.getDay())) count++;
      current.setDate(current.getDate() + 1);
    }
    return count;
  }, [genForm.start_date, genForm.end_date, genForm.days_of_week]);

  if (loading) {
    return (
      <div className="p-6 lg:p-8 max-w-7xl mx-auto">
        <div className="animate-pulse space-y-6">
          <div className="flex items-center gap-4">
            <div className="w-10 h-10 bg-slate-200 rounded-full" />
            <div className="w-12 h-12 bg-slate-200 rounded-xl" />
            <div className="flex-1">
              <div className="h-6 bg-slate-200 rounded w-1/3 mb-2" />
              <div className="h-4 bg-slate-100 rounded w-1/2" />
            </div>
          </div>
          <div className="h-12 bg-slate-100 rounded-xl" />
          <div className="h-96 bg-slate-100 rounded-xl" />
        </div>
      </div>
    );
  }

  if (!program) {
    return (
      <div className="p-6 text-center pt-20">
        <div className="w-16 h-16 rounded-2xl bg-slate-100 flex items-center justify-center mx-auto mb-4">
          <GraduationCap className="w-8 h-8 text-slate-400" />
        </div>
        <h2 className="text-xl font-bold text-slate-800 mb-2">Programa no encontrado</h2>
        <button onClick={() => router.push('/dashboard/programs')} className="mt-2 text-emerald-600 hover:underline font-medium">
          Volver a Programas
        </button>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col min-h-0 overflow-hidden gap-3">
      {/* Header */}
      <div className="flex items-center gap-3 shrink-0">
        <button
          onClick={() => router.push('/dashboard/programs')}
          className="p-2 hover:bg-slate-100 rounded-xl transition-colors shrink-0"
        >
          <ArrowLeft className="w-5 h-5 text-slate-600" />
        </button>
        <div
          className="w-9 h-9 rounded-xl flex items-center justify-center text-white font-bold text-base shadow-sm shrink-0"
          style={{ backgroundColor: program.color || '#10b981' }}
        >
          {program.name.charAt(0).toUpperCase()}
        </div>
        <div className="flex-1 min-w-0">
          <h1 className="text-lg font-bold text-slate-800 truncate leading-tight">{program.name}</h1>
          <p className="text-slate-500 text-xs truncate">{program.description || 'Sin descripción'}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowCampaignModal(true)}
            disabled={participantsWithPhone.length === 0}
            className="hidden sm:flex items-center gap-2 px-4 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Send className="w-4 h-4" />
            Envío Masivo
          </button>
          <button
            onClick={openEditModal}
            className="p-2.5 hover:bg-slate-100 rounded-xl transition-colors text-slate-500 hover:text-slate-700"
            title="Editar programa"
          >
            <Edit2 className="w-4 h-4" />
          </button>
          <div className="relative">
            <button
              onClick={() => setShowHeaderMenu(!showHeaderMenu)}
              className="p-2.5 hover:bg-slate-100 rounded-xl transition-colors text-slate-500 hover:text-slate-700"
              title="Más opciones"
            >
              <MoreVertical className="w-4 h-4" />
            </button>
            {showHeaderMenu && (
              <div className="absolute right-0 top-full mt-1 bg-white rounded-xl border border-slate-200 shadow-lg py-1 w-48 z-50">
                <button
                  onClick={handleArchiveProgram}
                  className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                >
                  <Archive className="w-4 h-4" />
                  {program.status === 'archived' ? 'Desarchivar' : 'Archivar'}
                </button>
                <button
                  onClick={handleDeleteProgram}
                  className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-red-600 hover:bg-red-50 transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                  Eliminar Programa
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Compact Stats Bar */}
      <div className="flex items-center gap-3 lg:gap-5 bg-white border border-slate-200 rounded-xl px-4 py-2.5 shrink-0 text-sm flex-wrap">
        <span className="flex items-center gap-1.5 text-slate-600">
          <Users className="w-3.5 h-3.5 text-purple-500" />
          <strong className="text-slate-800 font-semibold">{participants.length}</strong>
          <span className="hidden sm:inline">participantes</span>
        </span>
        <span className="text-slate-200">|</span>
        <span className="flex items-center gap-1.5 text-slate-600">
          <Calendar className="w-3.5 h-3.5 text-blue-500" />
          <strong className="text-slate-800 font-semibold">{sessions.length}</strong>
          <span className="hidden sm:inline">sesiones</span>
        </span>
        <span className="text-slate-200">|</span>
        <span className="flex items-center gap-1.5 text-slate-600">
          <Phone className="w-3.5 h-3.5 text-emerald-500" />
          <strong className="text-slate-800 font-semibold">{participantsWithPhone.length}</strong>
          <span className="hidden sm:inline">con teléfono</span>
        </span>
        <span className="text-slate-200">|</span>
        <span className="flex items-center gap-1.5 text-slate-600">
          <CheckCircle2 className="w-3.5 h-3.5 text-amber-500" />
          <strong className="text-slate-800 font-semibold">{sessions.reduce((sum, s) => sum + (s.attendance_stats?.present || 0), 0)}</strong>
          <span className="hidden sm:inline">asistencias</span>
        </span>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-slate-100 rounded-xl p-1 shrink-0">
        <button
          onClick={() => setActiveTab('participants')}
          className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg font-medium text-sm transition-all ${
            activeTab === 'participants'
              ? 'bg-white text-slate-800 shadow-sm'
              : 'text-slate-500 hover:text-slate-700'
          }`}
        >
          <Users className="w-4 h-4" />
          Participantes ({participants.length})
        </button>
        {program?.type === 'event' ? (
          <button
            onClick={() => setActiveTab('kanban')}
            className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg font-medium text-sm transition-all ${
              activeTab === 'kanban'
                ? 'bg-white text-slate-800 shadow-sm'
                : 'text-slate-500 hover:text-slate-700'
            }`}
          >
            <LayoutGrid className="w-4 h-4" />
            Kanban ({stages.length})
          </button>
        ) : (
          <>
            <button
              onClick={() => setActiveTab('sessions')}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg font-medium text-sm transition-all ${
                activeTab === 'sessions'
                  ? 'bg-white text-slate-800 shadow-sm'
                  : 'text-slate-500 hover:text-slate-700'
              }`}
            >
              <Calendar className="w-4 h-4" />
              Sesiones ({sessions.length})
            </button>
            <button
              onClick={() => setActiveTab('stats')}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg font-medium text-sm transition-all ${
                activeTab === 'stats'
                  ? 'bg-white text-slate-800 shadow-sm'
                  : 'text-slate-500 hover:text-slate-700'
              }`}
            >
              <BarChart3 className="w-4 h-4" />
              Estadísticas
            </button>
          </>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-h-0 overflow-hidden">
      {activeTab === 'participants' ? (
        <div className="h-full flex flex-col gap-3">
          <div className="flex justify-between items-center shrink-0">
            <h2 className="text-base font-semibold text-slate-800">Lista de Participantes</h2>
            <div className="flex items-center gap-2">
              <div className="relative">
                <button
                  onClick={() => setShowColumnPicker(v => !v)}
                  className="flex items-center gap-2 px-3 py-2 border border-slate-200 text-slate-600 rounded-xl hover:bg-slate-50 transition-all text-sm font-medium"
                  title="Personalizar columnas"
                >
                  <Columns3 className="w-4 h-4" />
                  <span className="hidden sm:inline">Columnas</span>
                </button>
                {showColumnPicker && (
                  <>
                    <div className="fixed inset-0 z-40" onClick={() => setShowColumnPicker(false)} />
                    <div className="absolute right-0 top-full mt-1 z-50 bg-white border border-slate-200 rounded-xl shadow-lg py-1 min-w-[200px]" onMouseDown={e => e.stopPropagation()}>
                      <p className="px-3 py-1.5 text-xs font-semibold text-slate-400 uppercase tracking-wider">Columnas visibles</p>
                      {PARTICIPANT_COLUMNS.map(col => {
                        const isVisible = visibleColumns.includes(col.id);
                        return (
                          <label
                            key={col.id}
                            className={`flex items-center gap-2.5 px-3 py-2 text-sm transition-colors ${col.always ? 'text-slate-400 cursor-not-allowed' : 'text-slate-700 hover:bg-slate-50 cursor-pointer'}`}
                          >
                            <input
                              type="checkbox"
                              checked={isVisible}
                              disabled={col.always}
                              onChange={() => toggleColumn(col.id)}
                              className="w-4 h-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500 focus:ring-offset-0"
                            />
                            <span className="flex-1">{col.label}</span>
                            {col.always && <span className="text-[10px] text-slate-400">fijo</span>}
                          </label>
                        );
                      })}
                    </div>
                  </>
                )}
              </div>
              <button
                onClick={() => setIsAddParticipantOpen(true)}
                className="flex items-center gap-2 px-3 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all text-sm font-medium shadow-sm"
              >
                <Plus className="w-4 h-4" />
                Agregar
              </button>
            </div>
          </div>

          <div className="flex-1 min-h-0 bg-white rounded-xl border border-slate-200 overflow-hidden">
            <div className="h-full overflow-x-auto">
              <div className="h-full overflow-y-auto">
                <table className="w-full text-left text-sm">
                  <thead className="bg-slate-50 text-slate-600 border-b border-slate-200 sticky top-0 z-10">
                    <tr>
                      {PARTICIPANT_COLUMNS.filter(c => visibleColumns.includes(c.id)).map(col => (
                        <th key={col.id} className={`px-5 py-3 font-medium ${col.id === 'actions' ? 'text-right' : ''}`}>{col.label}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {participants.length === 0 ? (
                      <tr>
                        <td colSpan={visibleColumns.length} className="px-5 py-12 text-center">
                          <Users className="w-10 h-10 text-slate-300 mx-auto mb-3" />
                          <p className="text-slate-500 font-medium">Sin participantes</p>
                          <p className="text-slate-400 text-xs mt-1">Agrega participantes para comenzar</p>
                        </td>
                      </tr>
                    ) : (
                      participants.map((p) => {
                        const isSelected = selectedParticipantID === p.id;
                        return (
                        <tr
                          key={p.id}
                          className={`transition-colors cursor-pointer ${isSelected ? 'bg-emerald-50 hover:bg-emerald-50' : 'hover:bg-slate-50'}`}
                          onClick={() => handleParticipantClick(p)}
                        >
                          {PARTICIPANT_COLUMNS.filter(c => visibleColumns.includes(c.id)).map(col => {
                            if (col.id === 'name') {
                              return (
                                <td key={col.id} className={`px-5 py-3 ${isSelected ? 'border-l-4 border-emerald-500 pl-4' : ''}`}>
                                  <div className="flex items-center gap-2.5">
                                    <div className={`w-8 h-8 rounded-full flex items-center justify-center font-medium text-xs ${isSelected ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'}`}>
                                      {(p.contact_name || '?').charAt(0).toUpperCase()}
                                    </div>
                                    <span className={`font-medium ${isSelected ? 'text-emerald-800' : 'text-slate-800'}`}>{p.contact_name || 'Sin nombre'}</span>
                                    {isSelected && loadingLead && <span className="text-xs text-emerald-500">...</span>}
                                  </div>
                                </td>
                              );
                            }
                            if (col.id === 'phone') {
                              return (
                                <td key={col.id} className="px-5 py-3 text-slate-600 font-mono text-xs">
                                  {p.contact_phone || <span className="text-slate-400 italic">Sin teléfono</span>}
                                </td>
                              );
                            }
                            if (col.id === 'status') {
                              return (
                                <td key={col.id} className="px-5 py-3">
                                  <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${
                                    p.status === 'active' ? 'bg-emerald-50 text-emerald-700' :
                                    p.status === 'completed' ? 'bg-blue-50 text-blue-700' :
                                    'bg-red-50 text-red-700'
                                  }`}>
                                    {p.status === 'active' ? 'Activo' : p.status === 'completed' ? 'Completado' : 'Retirado'}
                                  </span>
                                </td>
                              );
                            }
                            if (col.id === 'enrolled_at') {
                              return (
                                <td key={col.id} className="px-5 py-3 text-slate-500 text-xs">
                                  {format(new Date(p.enrolled_at), 'dd MMM yyyy', { locale: es })}
                                </td>
                              );
                            }
                            if (col.id === 'actions') {
                              return (
                                <td key={col.id} className="px-5 py-3 text-right">
                                  <button
                                    onClick={(e) => { e.stopPropagation(); handleRemoveParticipant(p.id); }}
                                    className="p-1.5 rounded-lg hover:bg-red-50 text-slate-400 hover:text-red-500 transition-all"
                                    title="Eliminar participante"
                                  >
                                    <Trash2 className="w-4 h-4" />
                                  </button>
                                </td>
                              );
                            }
                            return <td key={col.id} className="px-5 py-3" />;
                          })}
                        </tr>
                        );
                      })
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      ) : activeTab === 'kanban' ? (
        <div className="h-full flex flex-col gap-3">
          <div className="flex justify-between items-center shrink-0">
            <h2 className="text-base font-semibold text-slate-800">Kanban de Participantes</h2>
            <button
              onClick={() => setIsAddParticipantOpen(true)}
              className="flex items-center gap-2 px-3.5 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all text-sm font-medium shadow-sm"
            >
              <Plus className="w-4 h-4" /> Agregar
            </button>
          </div>
          {stages.length === 0 ? (
            <div className="flex-1 flex items-center justify-center text-slate-500 text-sm">
              <div className="text-center">
                <AlertCircle className="w-8 h-8 mx-auto mb-2 text-amber-500" />
                <p>Este evento no tiene etapas configuradas.</p>
                <p className="text-xs mt-1">Edita el pipeline en Eventos → Pipelines.</p>
              </div>
            </div>
          ) : (
            <div className="flex-1 min-h-0 overflow-x-auto overflow-y-hidden">
              <div className="flex gap-3 h-full pb-2" style={{ minWidth: 'max-content' }}>
                {/* Stage: sin etapa */}
                {(() => {
                  const unstaged = participants.filter(p => !p.stage_id);
                  return (
                    <div
                      onDragOver={e => handleStageDragOver(e, '__unassigned__')}
                      onDragLeave={handleStageDragLeave}
                      onDrop={e => handleStageDrop(e, null)}
                      className={`flex flex-col w-72 shrink-0 bg-slate-50 border-2 rounded-xl transition-colors ${
                        dragOverStageID === '__unassigned__' ? 'border-emerald-400 bg-emerald-50' : 'border-slate-200'
                      }`}
                    >
                      <div className="p-3 border-b border-slate-200 flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <div className="w-2.5 h-2.5 rounded-full bg-slate-400" />
                          <span className="font-semibold text-slate-700 text-sm">Sin etapa</span>
                        </div>
                        <span className="text-xs text-slate-500 font-medium bg-white px-2 py-0.5 rounded-full">{unstaged.length}</span>
                      </div>
                      <div className="flex-1 overflow-y-auto p-2 space-y-2">
                        {unstaged.map(p => (
                          <div
                            key={p.id}
                            draggable
                            onDragStart={e => handleStageDragStart(e, p.id)}
                            className={`bg-white border border-slate-200 rounded-lg p-2.5 shadow-sm hover:shadow-md hover:border-emerald-300 transition-all cursor-move ${
                              draggedParticipantID === p.id ? 'opacity-40' : ''
                            }`}
                          >
                            <div className="font-medium text-slate-800 text-sm truncate">{p.contact_name || 'Sin nombre'}</div>
                            {p.contact_phone && (
                              <div className="text-xs text-slate-500 flex items-center gap-1 mt-1">
                                <Phone className="w-3 h-3" /> {p.contact_phone}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  );
                })()}
                {/* Stages */}
                {stages.sort((a, b) => a.position - b.position).map(stage => {
                  const stageParts = participants.filter(p => p.stage_id === stage.id);
                  return (
                    <div
                      key={stage.id}
                      onDragOver={e => handleStageDragOver(e, stage.id)}
                      onDragLeave={handleStageDragLeave}
                      onDrop={e => handleStageDrop(e, stage.id)}
                      className={`flex flex-col w-72 shrink-0 bg-slate-50 border-2 rounded-xl transition-colors ${
                        dragOverStageID === stage.id ? 'border-emerald-400 bg-emerald-50' : 'border-slate-200'
                      }`}
                    >
                      <div className="p-3 border-b border-slate-200 flex items-center justify-between">
                        <div className="flex items-center gap-2 min-w-0">
                          <div className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: stage.color || '#64748b' }} />
                          <span className="font-semibold text-slate-700 text-sm truncate">{stage.name}</span>
                        </div>
                        <span className="text-xs text-slate-500 font-medium bg-white px-2 py-0.5 rounded-full shrink-0 ml-2">{stageParts.length}</span>
                      </div>
                      <div className="flex-1 overflow-y-auto p-2 space-y-2">
                        {stageParts.map(p => (
                          <div
                            key={p.id}
                            draggable
                            onDragStart={e => handleStageDragStart(e, p.id)}
                            className={`bg-white border border-slate-200 rounded-lg p-2.5 shadow-sm hover:shadow-md hover:border-emerald-300 transition-all cursor-move ${
                              draggedParticipantID === p.id ? 'opacity-40' : ''
                            }`}
                          >
                            <div className="font-medium text-slate-800 text-sm truncate">{p.contact_name || 'Sin nombre'}</div>
                            {p.contact_phone && (
                              <div className="text-xs text-slate-500 flex items-center gap-1 mt-1">
                                <Phone className="w-3 h-3" /> {p.contact_phone}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      ) : activeTab === 'sessions' ? (
        <div className="h-full flex flex-col gap-3">
          <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3 shrink-0">
            <h2 className="text-base font-semibold text-slate-800">Sesiones y Asistencia</h2>
            <div className="flex gap-2">
              <button
                onClick={() => setIsGenerateSessionsOpen(true)}
                className="flex items-center gap-2 px-4 py-2 border border-emerald-200 text-emerald-700 bg-emerald-50 rounded-xl hover:bg-emerald-100 transition-all text-sm font-medium"
              >
                <Repeat className="w-4 h-4" />
                Generar Horario
              </button>
              <button
                onClick={() => setIsCreateSessionOpen(true)}
                className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all text-sm font-medium shadow-sm"
              >
                <Plus className="w-4 h-4" />
                Nueva Sesión
              </button>
            </div>
          </div>

          <div className="flex-1 min-h-0 overflow-y-auto">
          {sessions.length === 0 ? (
            <div className="text-center py-16 bg-white rounded-2xl border border-slate-200">
              <div className="w-16 h-16 rounded-2xl bg-blue-50 flex items-center justify-center mx-auto mb-4">
                <Calendar className="w-8 h-8 text-blue-400" />
              </div>
              <h3 className="text-lg font-semibold text-slate-800 mb-2">Sin sesiones programadas</h3>
              <p className="text-slate-500 mb-5 max-w-md mx-auto text-sm">
                Crea sesiones individuales o genera un horario recurrente estilo Google Calendar.
              </p>
              <div className="flex gap-3 justify-center">
                <button
                  onClick={() => setIsGenerateSessionsOpen(true)}
                  className="flex items-center gap-2 px-4 py-2 border border-emerald-200 text-emerald-700 bg-emerald-50 rounded-xl hover:bg-emerald-100 transition-all text-sm font-medium"
                >
                  <Repeat className="w-4 h-4" />
                  Generar Horario
                </button>
                <button
                  onClick={() => setIsCreateSessionOpen(true)}
                  className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all text-sm font-medium shadow-sm"
                >
                  <Plus className="w-4 h-4" />
                  Sesión Individual
                </button>
              </div>
            </div>
          ) : (
            <div className="space-y-3 pb-2">
              {sessions.map((session, idx) => {
                const totalAtt = (session.attendance_stats?.present || 0) + (session.attendance_stats?.absent || 0) + (session.attendance_stats?.late || 0) + (session.attendance_stats?.excused || 0);
                const isPast = new Date(session.date) < new Date();
                return (
                  <div
                    key={session.id}
                    className={`bg-white rounded-xl border border-slate-200 p-4 hover:shadow-md transition-all group ${isPast ? 'opacity-80' : ''}`}
                  >
                    <div className="flex items-center gap-4">
                      {/* Session number & date */}
                      <div className="hidden sm:flex flex-col items-center justify-center w-14 h-14 rounded-xl bg-slate-50 border border-slate-100 shrink-0">
                        <span className="text-xs text-slate-400 font-medium uppercase">
                          {format(new Date(session.date), 'MMM', { locale: es })}
                        </span>
                        <span className="text-lg font-bold text-slate-800 -mt-0.5">
                          {format(new Date(session.date), 'dd')}
                        </span>
                      </div>

                      {/* Session info */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-0.5">
                          <h3 className="font-semibold text-slate-800 truncate">
                            {session.topic || `Sesión ${idx + 1}`}
                          </h3>
                          {isPast && totalAtt === 0 && (
                            <span className="text-xs px-2 py-0.5 bg-amber-50 text-amber-600 rounded-full shrink-0">
                              Sin registrar
                            </span>
                          )}
                        </div>
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500">
                          <span className="flex items-center gap-1">
                            <CalendarDays className="w-3 h-3" />
                            {format(new Date(session.date), "EEEE, d 'de' MMMM", { locale: es })}
                          </span>
                          {session.start_time && (
                            <span className="flex items-center gap-1">
                              <Clock className="w-3 h-3" />
                              {session.start_time}{session.end_time ? ` - ${session.end_time}` : ''}
                            </span>
                          )}
                          {session.location && (
                            <span className="flex items-center gap-1">
                              <MapPin className="w-3 h-3" />
                              {session.location}
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Attendance stats */}
                      <div className="hidden md:flex items-center gap-1.5 text-xs shrink-0">
                        {totalAtt > 0 ? (
                          <>
                            <div className="flex items-center gap-0.5 text-emerald-600 bg-emerald-50 px-2 py-1 rounded-lg" title="Presentes">
                              <Check className="w-3 h-3" /> {session.attendance_stats?.present || 0}
                            </div>
                            <div className="flex items-center gap-0.5 text-red-600 bg-red-50 px-2 py-1 rounded-lg" title="Ausentes">
                              <X className="w-3 h-3" /> {session.attendance_stats?.absent || 0}
                            </div>
                            <div className="flex items-center gap-0.5 text-amber-600 bg-amber-50 px-2 py-1 rounded-lg" title="Tardes">
                              <Clock className="w-3 h-3" /> {session.attendance_stats?.late || 0}
                            </div>
                          </>
                        ) : (
                          <span className="text-slate-400 px-2 py-1 bg-slate-50 rounded-lg">Sin asistencia</span>
                        )}
                      </div>

                      {/* Actions */}
                      <div className="flex items-center gap-1 shrink-0">
                        <button
                          onClick={() => openAttendance(session)}
                          className="px-3 py-1.5 bg-slate-100 text-slate-700 rounded-lg hover:bg-slate-200 transition-colors text-xs font-medium"
                        >
                          Asistencia
                        </button>
                        <button
                          onClick={() => openEditSession(session)}
                          className="p-1.5 rounded-lg opacity-0 group-hover:opacity-100 transition-all hover:bg-slate-100 text-slate-400 hover:text-slate-600"
                          title="Editar sesión"
                        >
                          <Edit2 className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleDeleteSession(session.id)}
                          className="p-1.5 rounded-lg opacity-0 group-hover:opacity-100 transition-all hover:bg-red-50 text-slate-400 hover:text-red-500"
                          title="Eliminar sesión"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          </div>
        </div>
      ) : (
        /* Stats Tab */
        <div className="h-full overflow-y-auto">
          {loadingStats ? (
            <div className="flex items-center justify-center py-20">
              <div className="w-8 h-8 border-4 border-emerald-200 border-t-emerald-600 rounded-full animate-spin" />
            </div>
          ) : !statsData || (statsData.session_stats.length === 0 && statsData.participant_stats.length === 0) ? (
            <div className="text-center py-20">
              <div className="w-16 h-16 rounded-2xl bg-slate-100 flex items-center justify-center mx-auto mb-4">
                <BarChart3 className="w-8 h-8 text-slate-400" />
              </div>
              <h3 className="font-semibold text-slate-700 mb-1">Sin datos de asistencia</h3>
              <p className="text-sm text-slate-500">Registra asistencia en las sesiones para ver estadísticas.</p>
            </div>
          ) : (
            <div className="space-y-6 pb-4">
              {/* Month filter */}
              {(() => {
                // Extract available months from ALL sessions (not just filtered stats)
                const allMonths: string[] = [];
                for (const s of sessions) {
                  if (s.date) {
                    const m = s.date.substring(0, 7); // YYYY-MM
                    if (!allMonths.includes(m)) allMonths.push(m);
                  }
                }
                allMonths.sort();
                if (allMonths.length <= 1) return null;
                const monthNames: Record<string, string> = {
                  '01': 'Ene', '02': 'Feb', '03': 'Mar', '04': 'Abr', '05': 'May', '06': 'Jun',
                  '07': 'Jul', '08': 'Ago', '09': 'Sep', '10': 'Oct', '11': 'Nov', '12': 'Dic'
                };
                const toggleMonth = (m: string) => {
                  setSelectedMonths(prev => prev.includes(m) ? prev.filter(x => x !== m) : [...prev, m]);
                };
                return (
                  <div className="bg-white rounded-xl border border-slate-200 p-4">
                    <div className="flex items-center justify-between mb-3">
                      <h3 className="font-semibold text-slate-800 text-sm flex items-center gap-2">
                        <Calendar className="w-4 h-4 text-emerald-500" />
                        Filtrar por mes
                      </h3>
                      {selectedMonths.length > 0 && (
                        <button
                          onClick={() => setSelectedMonths([])}
                          className="text-xs text-emerald-600 hover:text-emerald-700 font-medium hover:underline transition-colors"
                        >
                          Limpiar filtros
                        </button>
                      )}
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {allMonths.map(m => {
                        const [year, month] = m.split('-');
                        const label = `${monthNames[month] || month} ${year}`;
                        const isSelected = selectedMonths.includes(m);
                        return (
                          <button
                            key={m}
                            onClick={() => toggleMonth(m)}
                            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all border ${
                              isSelected
                                ? 'bg-emerald-500 text-white border-emerald-500 shadow-sm shadow-emerald-200'
                                : 'bg-white text-slate-600 border-slate-200 hover:border-emerald-300 hover:text-emerald-600'
                            }`}
                          >
                            {label}
                          </button>
                        );
                      })}
                    </div>
                    {selectedMonths.length === 0 && (
                      <p className="text-[11px] text-slate-400 mt-2">Sin filtro = sesiones hasta hoy</p>
                    )}
                  </div>
                );
              })()}

              {/* Summary cards */}
              {(() => {
                const totalPresent = statsData.session_stats.reduce((s, ss) => s + (ss.present || 0), 0);
                const totalAbsent = statsData.session_stats.reduce((s, ss) => s + (ss.absent || 0), 0);
                const totalLate = statsData.session_stats.reduce((s, ss) => s + (ss.late || 0), 0);
                const totalExcused = statsData.session_stats.reduce((s, ss) => s + (ss.excused || 0), 0);
                const totalAll = totalPresent + totalAbsent + totalLate + totalExcused;
                const avgRate = totalAll > 0 ? Math.round(((totalPresent + totalLate) / totalAll) * 100) : 0;
                return (
                  <div className="grid grid-cols-2 lg:grid-cols-5 gap-3">
                    <div className="bg-white rounded-xl border border-slate-200 p-4">
                      <p className="text-xs text-slate-500 mb-1">Tasa promedio</p>
                      <p className="text-2xl font-bold text-emerald-600">{avgRate}%</p>
                    </div>
                    <div className="bg-white rounded-xl border border-slate-200 p-4">
                      <p className="text-xs text-slate-500 mb-1">Presentes</p>
                      <p className="text-2xl font-bold text-emerald-600">{totalPresent}</p>
                    </div>
                    <div className="bg-white rounded-xl border border-slate-200 p-4">
                      <p className="text-xs text-slate-500 mb-1">Ausentes</p>
                      <p className="text-2xl font-bold text-red-500">{totalAbsent}</p>
                    </div>
                    <div className="bg-white rounded-xl border border-slate-200 p-4">
                      <p className="text-xs text-slate-500 mb-1">Tardanzas</p>
                      <p className="text-2xl font-bold text-amber-500">{totalLate}</p>
                    </div>
                    <div className="bg-white rounded-xl border border-slate-200 p-4">
                      <p className="text-xs text-slate-500 mb-1">Justificados</p>
                      <p className="text-2xl font-bold text-blue-500">{totalExcused}</p>
                    </div>
                  </div>
                );
              })()}

              {/* Bar chart — Attendance per session */}
              <div className="bg-white rounded-xl border border-slate-200 p-5">
                <h3 className="font-semibold text-slate-800 mb-4 text-sm">Asistencia por sesión</h3>
                <div className="space-y-3">
                  {statsData.session_stats.map((ss: any, i: number) => {
                    const total = (ss.present || 0) + (ss.absent || 0) + (ss.late || 0) + (ss.excused || 0);
                    const pPct = total > 0 ? ((ss.present || 0) / total) * 100 : 0;
                    const lPct = total > 0 ? ((ss.late || 0) / total) * 100 : 0;
                    const ePct = total > 0 ? ((ss.excused || 0) / total) * 100 : 0;
                    const aPct = total > 0 ? ((ss.absent || 0) / total) * 100 : 0;
                    const label = ss.topic || (ss.date ? format(new Date(ss.date), 'dd MMM', { locale: es }) : `Sesión ${i + 1}`);
                    return (
                      <div key={ss.session_id || i}>
                        <div className="flex items-center justify-between mb-1">
                          <span className="text-xs text-slate-600 font-medium truncate max-w-[200px]">{label}</span>
                          <span className="text-xs text-slate-400">{Math.round(pPct + lPct)}% asistencia</span>
                        </div>
                        <div className="flex h-5 rounded-lg overflow-hidden bg-slate-100">
                          {pPct > 0 && <div className="bg-emerald-500 transition-all" style={{ width: `${pPct}%` }} title={`Presentes: ${ss.present}`} />}
                          {lPct > 0 && <div className="bg-amber-400 transition-all" style={{ width: `${lPct}%` }} title={`Tardanzas: ${ss.late}`} />}
                          {ePct > 0 && <div className="bg-blue-400 transition-all" style={{ width: `${ePct}%` }} title={`Justificados: ${ss.excused}`} />}
                          {aPct > 0 && <div className="bg-red-400 transition-all" style={{ width: `${aPct}%` }} title={`Ausentes: ${ss.absent}`} />}
                        </div>
                      </div>
                    );
                  })}
                </div>
                {/* Legend */}
                <div className="flex gap-4 mt-4 text-xs text-slate-500">
                  <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded bg-emerald-500 inline-block" />Presente</span>
                  <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded bg-amber-400 inline-block" />Tardanza</span>
                  <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded bg-blue-400 inline-block" />Justificado</span>
                  <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded bg-red-400 inline-block" />Ausente</span>
                </div>
              </div>

              {/* Trend line */}
              {statsData.session_stats.length > 1 && (
                <div className="bg-white rounded-xl border border-slate-200 p-5">
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="font-semibold text-slate-800 text-sm">Tendencia de asistencia</h3>
                    <span className="text-[11px] text-slate-400">{statsData.session_stats.length} sesiones · desliza →</span>
                  </div>
                  <div className="overflow-x-auto pb-2" style={{ scrollbarWidth: 'thin', scrollbarColor: '#cbd5e1 transparent' }}>
                    <div className="h-48 flex items-end gap-2" style={{ minWidth: `${Math.max(statsData.session_stats.length * 52, 300)}px` }}>
                      {statsData.session_stats.map((ss: any, i: number) => {
                        const total = (ss.present || 0) + (ss.absent || 0) + (ss.late || 0) + (ss.excused || 0);
                        const rate = total > 0 ? Math.round(((ss.present || 0) + (ss.late || 0)) / total * 100) : 0;
                        const label = ss.topic || (ss.date ? format(new Date(ss.date), 'dd/MM', { locale: es }) : `S${i + 1}`);
                        return (
                          <div key={ss.session_id || i} className="flex flex-col items-center gap-1 w-[44px] shrink-0">
                            <span className="text-[10px] font-semibold text-slate-700">{rate}%</span>
                            <div className="w-8 rounded-t-lg transition-all" style={{
                              height: `${Math.max(rate * 1.6, 4)}px`,
                              backgroundColor: rate >= 80 ? '#10b981' : rate >= 50 ? '#f59e0b' : '#ef4444'
                            }} />
                            <span className="text-[9px] text-slate-400 truncate w-[44px] text-center">{label}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                </div>
              )}

              {/* Participant ranking (thermometer) */}
              {statsData.participant_stats.length > 0 && (
                <div className="bg-white rounded-xl border border-slate-200 p-5">
                  <h3 className="font-semibold text-slate-800 mb-4 text-sm">Ranking de asistencia por participante</h3>
                  <div className="space-y-2">
                    {statsData.participant_stats
                      .sort((a: any, b: any) => (b.rate || 0) - (a.rate || 0))
                      .map((ps: any, i: number) => {
                        const rate = ps.rate || 0;
                        const color = rate >= 80 ? 'bg-emerald-500' : rate >= 50 ? 'bg-amber-400' : 'bg-red-400';
                        const textColor = rate >= 80 ? 'text-emerald-700' : rate >= 50 ? 'text-amber-700' : 'text-red-600';
                        return (
                          <div key={ps.participant_id || i} className="flex items-center gap-3">
                            <span className="w-6 text-xs text-slate-400 text-right font-medium">{i + 1}</span>
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center justify-between mb-0.5">
                                <span className="text-sm font-medium text-slate-700 truncate">{ps.name || 'Sin nombre'}</span>
                                <span className={`text-xs font-bold ${textColor}`}>{Math.round(rate)}%</span>
                              </div>
                              <div className="h-2 bg-slate-100 rounded-full overflow-hidden">
                                <div className={`h-full ${color} rounded-full transition-all`} style={{ width: `${rate}%` }} />
                              </div>
                              <div className="flex gap-3 mt-0.5 text-[10px] text-slate-400">
                                <span>{ps.present || 0} presentes</span>
                                <span>{ps.late || 0} tardanzas</span>
                                <span>{ps.absent || 0} ausentes</span>
                                <span>{ps.total_sessions || 0} sesiones</span>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      )}
      </div>

      {/* =================== MODALS =================== */}

      {/* Add Participant Modal */}
      {isAddParticipantOpen && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 w-full max-w-2xl max-h-[90vh] flex flex-col shadow-2xl">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-xl font-bold text-slate-800">Agregar Participantes</h2>
              <button onClick={() => setIsAddParticipantOpen(false)} className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto min-h-[400px]">
              <ContactSelector
                open={isAddParticipantOpen}
                onClose={() => setIsAddParticipantOpen(false)}
                onConfirm={handleAddParticipants}
                title="Agregar Participantes"
                confirmLabel="Agregar Seleccionados"
                excludeIds={new Set(participants.map(p => p.contact_id))}
                sourceFilter="contact"
                advancedFilters
              />
            </div>
          </div>
        </div>
      )}

      {/* Create Session Modal */}
      {isCreateSessionOpen && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 w-full max-w-md shadow-2xl">
            <div className="flex justify-between items-center mb-5">
              <h2 className="text-xl font-bold text-slate-800">Nueva Sesión</h2>
              <button onClick={() => setIsCreateSessionOpen(false)} className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <form onSubmit={handleCreateSession}>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Fecha</label>
                  <input
                    type="date"
                    required
                    value={newSession.date}
                    onChange={(e) => setNewSession({ ...newSession, date: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Tema / Título</label>
                  <input
                    type="text"
                    required
                    value={newSession.topic}
                    onChange={(e) => setNewSession({ ...newSession, topic: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    placeholder="Ej: Introducción al curso"
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora inicio</label>
                    <input
                      type="time"
                      value={newSession.start_time}
                      onChange={(e) => setNewSession({ ...newSession, start_time: e.target.value })}
                      className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora fin</label>
                    <input
                      type="time"
                      value={newSession.end_time}
                      onChange={(e) => setNewSession({ ...newSession, end_time: e.target.value })}
                      className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Ubicación (opcional)</label>
                  <input
                    type="text"
                    value={newSession.location}
                    onChange={(e) => setNewSession({ ...newSession, location: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    placeholder="Ej: Sala A, Piso 2"
                  />
                </div>
              </div>
              <div className="flex justify-end gap-3 mt-6 pt-4 border-t border-slate-100">
                <button
                  type="button"
                  onClick={() => setIsCreateSessionOpen(false)}
                  className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium"
                >
                  Cancelar
                </button>
                <button
                  type="submit"
                  className="px-5 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium"
                >
                  Crear Sesión
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Edit Session Modal */}
      {editingSession && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 w-full max-w-md shadow-2xl">
            <div className="flex justify-between items-center mb-5">
              <h2 className="text-xl font-bold text-slate-800">Editar Sesión</h2>
              <button onClick={() => setEditingSession(null)} className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <form onSubmit={handleUpdateSession}>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Fecha</label>
                  <input
                    type="date"
                    required
                    value={editSessionForm.date}
                    onChange={(e) => setEditSessionForm({ ...editSessionForm, date: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Tema / Título</label>
                  <input
                    type="text"
                    required
                    value={editSessionForm.topic}
                    onChange={(e) => setEditSessionForm({ ...editSessionForm, topic: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    placeholder="Ej: Introducción al curso"
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora inicio</label>
                    <input
                      type="time"
                      value={editSessionForm.start_time}
                      onChange={(e) => setEditSessionForm({ ...editSessionForm, start_time: e.target.value })}
                      className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora fin</label>
                    <input
                      type="time"
                      value={editSessionForm.end_time}
                      onChange={(e) => setEditSessionForm({ ...editSessionForm, end_time: e.target.value })}
                      className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Ubicación (opcional)</label>
                  <input
                    type="text"
                    value={editSessionForm.location}
                    onChange={(e) => setEditSessionForm({ ...editSessionForm, location: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                    placeholder="Ej: Sala A, Piso 2"
                  />
                </div>
              </div>
              <div className="flex justify-end gap-3 mt-6 pt-4 border-t border-slate-100">
                <button
                  type="button"
                  onClick={() => setEditingSession(null)}
                  className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium"
                >
                  Cancelar
                </button>
                <button
                  type="submit"
                  disabled={savingSession}
                  className="px-5 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium disabled:opacity-50"
                >
                  {savingSession ? 'Guardando...' : 'Guardar Cambios'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Generate Sessions Modal - Google Calendar Style */}
      {isGenerateSessionsOpen && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 w-full max-w-lg shadow-2xl">
            <div className="flex justify-between items-center mb-5">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-xl bg-emerald-50 flex items-center justify-center">
                  <Repeat className="w-5 h-5 text-emerald-600" />
                </div>
                <div>
                  <h2 className="text-xl font-bold text-slate-800">Generar Horario Recurrente</h2>
                  <p className="text-xs text-slate-500">Configura la recurrencia y genera todas las sesiones</p>
                </div>
              </div>
              <button onClick={() => setIsGenerateSessionsOpen(false)} className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="space-y-5">
              {/* Date range */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Fecha inicio</label>
                  <input
                    type="date"
                    value={genForm.start_date}
                    onChange={(e) => setGenForm({ ...genForm, start_date: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Fecha fin</label>
                  <input
                    type="date"
                    value={genForm.end_date}
                    onChange={(e) => setGenForm({ ...genForm, end_date: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
              </div>

              {/* Days of week - Google Calendar style */}
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-2">Días de la semana</label>
                <div className="flex gap-2">
                  {DAY_NAMES.map((name, idx) => (
                    <button
                      key={idx}
                      type="button"
                      onClick={() => toggleGenDay(idx)}
                      className={`w-10 h-10 rounded-full text-sm font-medium transition-all ${
                        genForm.days_of_week.includes(idx)
                          ? 'bg-emerald-600 text-white shadow-sm'
                          : 'bg-slate-100 text-slate-500 hover:bg-slate-200'
                      }`}
                    >
                      {name.charAt(0)}
                    </button>
                  ))}
                </div>
                {genForm.days_of_week.length > 0 && (
                  <p className="text-xs text-slate-500 mt-2">
                    {genForm.days_of_week.map(d => DAY_NAMES_FULL[d]).join(', ')}
                  </p>
                )}
              </div>

              {/* Time range */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora inicio</label>
                  <input
                    type="time"
                    value={genForm.start_time}
                    onChange={(e) => setGenForm({ ...genForm, start_time: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1.5">Hora fin</label>
                  <input
                    type="time"
                    value={genForm.end_time}
                    onChange={(e) => setGenForm({ ...genForm, end_time: e.target.value })}
                    className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  />
                </div>
              </div>

              {/* Topic prefix */}
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1.5">Prefijo del tema</label>
                <input
                  type="text"
                  value={genForm.topic_prefix}
                  onChange={(e) => setGenForm({ ...genForm, topic_prefix: e.target.value })}
                  className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  placeholder="Ej: Sesión, Clase, Taller"
                />
                <p className="text-xs text-slate-400 mt-1">Se generará como &quot;{genForm.topic_prefix || 'Sesión'} 1&quot;, &quot;{genForm.topic_prefix || 'Sesión'} 2&quot;, etc.</p>
              </div>

              {/* Location */}
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1.5">Ubicación (opcional)</label>
                <input
                  type="text"
                  value={genForm.location}
                  onChange={(e) => setGenForm({ ...genForm, location: e.target.value })}
                  className="w-full px-3.5 py-2.5 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                  placeholder="Ej: Auditorio Principal"
                />
              </div>

              {/* Preview */}
              {previewSessionCount > 0 && (
                <div className="bg-emerald-50 border border-emerald-200 rounded-xl p-4">
                  <div className="flex items-center gap-2">
                    <CalendarDays className="w-5 h-5 text-emerald-600" />
                    <span className="font-semibold text-emerald-800 text-sm">
                      Se generarán {previewSessionCount} sesiones
                    </span>
                  </div>
                  <p className="text-xs text-emerald-600 mt-1">
                    Cada {genForm.days_of_week.map(d => DAY_NAMES_FULL[d]).join(', ')} de{' '}
                    {genForm.start_time || '—'} a {genForm.end_time || '—'}
                  </p>
                </div>
              )}
            </div>

            <div className="flex justify-end gap-3 mt-6 pt-4 border-t border-slate-100">
              <button
                onClick={() => setIsGenerateSessionsOpen(false)}
                className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium"
              >
                Cancelar
              </button>
              <button
                onClick={handleGenerateSessions}
                disabled={generating || previewSessionCount === 0}
                className="px-5 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
              >
                {generating ? (
                  <>
                    <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Generando...
                  </>
                ) : (
                  <>
                    <CalendarDays className="w-4 h-4" />
                    Generar {previewSessionCount} Sesiones
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Attendance Modal */}
      {isAttendanceOpen && selectedSession && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 w-full max-w-3xl max-h-[90vh] flex flex-col shadow-2xl">
            <div className="flex justify-between items-center mb-4">
              <div>
                <h2 className="text-xl font-bold text-slate-800">Tomar Asistencia</h2>
                <p className="text-sm text-slate-500">
                  {selectedSession.topic} — {format(new Date(selectedSession.date), "EEEE, d 'de' MMMM", { locale: es })}
                  {selectedSession.start_time && ` · ${selectedSession.start_time}`}
                </p>
              </div>
              <button onClick={() => setIsAttendanceOpen(false)} className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto pr-1">
              <table className="w-full text-left text-sm">
                <thead className="bg-slate-50 text-slate-600 sticky top-0 z-10 border-b border-slate-200">
                  <tr>
                    <th className="px-4 py-3 font-medium">Participante</th>
                    <th className="px-4 py-3 font-medium">Estado</th>
                    <th className="px-4 py-3 font-medium">Notas</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {participants.map((p) => (
                    <tr key={p.id} className="hover:bg-slate-50 transition-colors">
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <div className="w-7 h-7 rounded-full bg-slate-100 flex items-center justify-center text-slate-600 font-medium text-xs">
                            {(p.contact_name || '?').charAt(0).toUpperCase()}
                          </div>
                          <span className="font-medium text-slate-800 text-sm">{p.contact_name}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex gap-1">
                          {[
                            { key: 'present', label: 'P', color: 'emerald', title: 'Presente' },
                            { key: 'absent', label: 'A', color: 'red', title: 'Ausente' },
                            { key: 'late', label: 'T', color: 'amber', title: 'Tarde' },
                            { key: 'excused', label: 'J', color: 'blue', title: 'Justificado' },
                          ].map(opt => (
                            <button
                              key={opt.key}
                              type="button"
                              onClick={() => setAttendanceData({
                                ...attendanceData,
                                [p.id]: { ...attendanceData[p.id], status: opt.key }
                              })}
                              title={opt.title}
                              className={`w-8 h-8 rounded-lg text-xs font-bold transition-all ${
                                attendanceData[p.id]?.status === opt.key
                                  ? opt.color === 'emerald' ? 'bg-emerald-100 text-emerald-700 ring-2 ring-emerald-300'
                                  : opt.color === 'red' ? 'bg-red-100 text-red-700 ring-2 ring-red-300'
                                  : opt.color === 'amber' ? 'bg-amber-100 text-amber-700 ring-2 ring-amber-300'
                                  : 'bg-blue-100 text-blue-700 ring-2 ring-blue-300'
                                  : 'bg-slate-100 text-slate-400 hover:bg-slate-200'
                              }`}
                            >
                              {opt.label}
                            </button>
                          ))}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <input
                          type="text"
                          value={attendanceData[p.id]?.notes || ''}
                          onChange={(e) => setAttendanceData({
                            ...attendanceData,
                            [p.id]: { ...attendanceData[p.id], notes: e.target.value }
                          })}
                          placeholder="Opcional..."
                          className="w-full px-2.5 py-1.5 border border-slate-200 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="flex justify-end gap-3 mt-4 pt-4 border-t border-slate-100">
              <button
                onClick={() => setIsAttendanceOpen(false)}
                className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium"
              >
                Cancelar
              </button>
              <button
                onClick={saveAttendance}
                className="px-5 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium flex items-center gap-2"
              >
                <Check className="w-4 h-4" />
                Guardar Asistencia
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Campaign Modal */}
      <CreateCampaignModal
        open={showCampaignModal}
        onClose={() => setShowCampaignModal(false)}
        onSubmit={handleCreateCampaign}
        devices={devices}
        title="Envío Masivo desde Programa"
        subtitle="Crea una campaña con los participantes que tengan teléfono"
        accentColor="green"
        submitLabel={creatingCampaign ? 'Creando...' : `Crear campaña (${participantsWithPhone.length})`}
        submitting={creatingCampaign || participantsWithPhone.length === 0}
        initialName={`Envío - ${program?.name || ''}`}
        infoPanel={
          <div className="bg-emerald-50 border border-emerald-200 rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <Users className="w-4 h-4 text-emerald-600" />
              <span className="text-sm font-semibold text-emerald-800">
                {participantsWithPhone.length} destinatarios con teléfono
              </span>
            </div>
            <p className="text-xs text-emerald-600">
              Se enviará a todos los participantes del programa que tengan número de teléfono registrado.
            </p>
          </div>
        }
      />

      {/* Edit Program Modal */}
      {isEditModalOpen && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" onClick={() => setIsEditModalOpen(false)}>
          <div className="bg-white rounded-2xl w-full max-w-md shadow-2xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between p-5 border-b border-slate-100">
              <h3 className="text-lg font-bold text-slate-800">Editar Programa</h3>
              <button onClick={() => setIsEditModalOpen(false)} className="p-1.5 hover:bg-slate-100 rounded-lg transition-colors">
                <X className="w-5 h-5 text-slate-400" />
              </button>
            </div>
            <form onSubmit={handleUpdateProgram} className="p-5 space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Nombre</label>
                <input
                  type="text"
                  value={editForm.name}
                  onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
                  required
                  className="w-full px-3 py-2.5 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Descripción</label>
                <textarea
                  value={editForm.description}
                  onChange={(e) => setEditForm({ ...editForm, description: e.target.value })}
                  rows={3}
                  className="w-full px-3 py-2.5 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all resize-none"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Color</label>
                <div className="flex gap-2 flex-wrap">
                  {['#10b981', '#3b82f6', '#8b5cf6', '#6366f1', '#ec4899', '#f43f5e', '#f97316', '#f59e0b'].map(c => (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setEditForm({ ...editForm, color: c })}
                      className={`w-8 h-8 rounded-full transition-all ${editForm.color === c ? 'ring-2 ring-offset-2 ring-slate-400 scale-110' : 'hover:scale-110'}`}
                      style={{ backgroundColor: c }}
                    />
                  ))}
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Estado</label>
                <select
                  value={editForm.status}
                  onChange={(e) => setEditForm({ ...editForm, status: e.target.value })}
                  className="w-full px-3 py-2.5 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 transition-all"
                >
                  <option value="active">Activo</option>
                  <option value="archived">Archivado</option>
                  <option value="completed">Completado</option>
                </select>
              </div>
              <div className="flex justify-end gap-3 pt-2">
                <button type="button" onClick={() => setIsEditModalOpen(false)} className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium">
                  Cancelar
                </button>
                <button type="submit" disabled={saving || !editForm.name.trim()} className="px-5 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 transition-all shadow-sm font-medium disabled:opacity-50">
                  {saving ? 'Guardando...' : 'Guardar Cambios'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Lead/Contact Detail Side Panel with Inline Chat */}
      {selectedLead && (
        <div className="fixed inset-0 z-50 flex justify-end overflow-hidden">
          <div
            className="absolute inset-0 bg-black/30 backdrop-blur-[2px]"
            onClick={() => { setSelectedLead(null); setSelectedContact(null); setContactPanelMode(false); setShowInlineChat(false); setSelectedParticipantID(null); }}
          />
          <div className={`relative h-full bg-white shadow-2xl flex transition-all duration-300 border-l border-slate-200 ${showInlineChat ? 'w-[85vw] max-w-6xl' : 'w-full max-w-md'}`}>
            {/* Chat Panel - Left Side */}
            {showInlineChat && inlineChatId && (
              <div className="flex-1 min-w-0 border-r border-slate-200 flex flex-col h-full bg-slate-50/50">
                <ChatPanel
                  chatId={inlineChatId}
                  deviceId={inlineChatDeviceId}
                  initialChat={inlineChat || undefined}
                  readOnly={inlineChatReadOnly}
                  onClose={() => setShowInlineChat(false)}
                  className="h-full"
                />
              </div>
            )}
            {/* Detail Panel - Right Side (programs are contact-only by spec) */}
            <div className={`${showInlineChat ? 'w-[360px] shrink-0' : 'w-full'} flex flex-col h-full bg-white`}>
              {selectedContact && (
                <LeadDetailPanel
                  contactMode
                  contactId={selectedContact.id}
                  lead={contactToLead(selectedContact)}
                  pushName={selectedContact.push_name}
                  avatarUrl={selectedContact.avatar_url}
                  onLeadChange={() => {}}
                  onContactUpdate={(updated: any) => {
                    setSelectedContact(updated);
                    setSelectedLead(contactToLead(updated));
                    // Update participant list in-place without full page refresh
                    setParticipants(prev => prev.map(p =>
                      p.contact_id === updated.id
                        ? { ...p, contact_name: updated.custom_name || updated.name || p.contact_name, contact_phone: updated.phone || p.contact_phone }
                        : p
                    ));
                  }}
                  onClose={() => { setSelectedLead(null); setSelectedContact(null); setContactPanelMode(false); setSelectedParticipantID(null); }}
                  onSendWhatsApp={(phone: string) => handleSendWhatsApp(phone)}
                  onObservationChange={() => {}}
                  hideDelete
                  hideWhatsApp={showInlineChat}
                />
              )}
            </div>
          </div>
        </div>
      )}

      {/* Device Selector Modal for WhatsApp */}
      {showDeviceSelector && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-[60] p-4">
          <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-sm border border-slate-100">
            <h2 className="text-sm font-semibold text-slate-900 mb-3">Seleccionar dispositivo</h2>
            <p className="text-xs text-slate-500 mb-4">Elige el dispositivo para el chat con {whatsappPhone}</p>
            {existingChatForWA && (
              <p className="text-xs text-amber-700 bg-amber-50 border border-amber-100 rounded-lg px-3 py-2 mb-3">
                Ya existe historial{whatsappHistoricalPhone ? ` con el numero ${whatsappHistoricalPhone}` : ' con numero historico desconocido'}.
              </p>
            )}
            {devices.length === 0 ? (
              <p className="text-xs text-slate-400 text-center py-4">No hay dispositivos conectados</p>
            ) : (
              <div className="space-y-2">
                {devices.map(device => (
                  <button key={device.id} onClick={() => handleDeviceSelectedForChat(device)}
                    className="w-full flex items-center gap-3 p-3 border border-slate-100 rounded-xl hover:bg-emerald-50 hover:border-emerald-200 transition text-left"
                  >
                    <div className="w-9 h-9 bg-emerald-50 rounded-full flex items-center justify-center"><Phone className="w-4 h-4 text-emerald-600" /></div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-medium text-slate-900">{device.name || 'Dispositivo'}</p>
                        <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${relationClassName(device)}`}>{relationLabel(device)}</span>
                      </div>
                      <p className="text-xs text-slate-500">{deviceDisplayPhone(device)}</p>
                    </div>
                  </button>
                ))}
              </div>
            )}
            <button onClick={() => setShowDeviceSelector(false)} className="w-full mt-4 px-4 py-2 border border-slate-200 text-slate-600 rounded-xl hover:bg-slate-50 text-sm">Cancelar</button>
          </div>
        </div>
      )}

      {/* Toast */}
      {toastMessage && (
        <div className={`fixed bottom-6 right-6 z-[70] flex items-center gap-2.5 px-5 py-3 rounded-xl shadow-lg text-sm font-medium transition-all animate-in slide-in-from-bottom-4 ${
          toastType === 'success' ? 'bg-emerald-600 text-white' : 'bg-red-600 text-white'
        }`}>
          {toastType === 'success' ? <CheckCircle2 className="w-4 h-4" /> : <AlertCircle className="w-4 h-4" />}
          {toastMessage}
          <button onClick={() => setToastMessage(null)} className="ml-1 p-0.5 hover:bg-white/20 rounded">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Confirmation Dialog */}
      {confirmAction && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-[70] p-4">
          <div className="bg-white rounded-2xl shadow-2xl w-full max-w-sm p-6">
            <div className="flex items-center gap-3 mb-4">
              <div className="w-10 h-10 rounded-full bg-red-50 flex items-center justify-center shrink-0">
                <AlertCircle className="w-5 h-5 text-red-500" />
              </div>
              <p className="text-sm text-slate-700 leading-relaxed">{confirmAction.message}</p>
            </div>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setConfirmAction(null)}
                className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-xl transition-colors font-medium text-sm"
              >
                Cancelar
              </button>
              <button
                onClick={confirmAction.onConfirm}
                className="px-5 py-2.5 bg-red-600 text-white rounded-xl hover:bg-red-700 transition-all shadow-sm font-medium text-sm"
              >
                Confirmar
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

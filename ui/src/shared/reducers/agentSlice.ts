import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit';
import {
  AgentModeRequestModeEnum,
  AgentStatusModeEnum,
  AgentStatusConsoleConnectionEnum,
} from '@generated/index';
import { apiClient } from '@shared/api/client';

function capitalizeFirst(str: string): string {
  if (!str) return str;
  return str.charAt(0).toUpperCase() + str.slice(1);
}

interface AgentState {
  mode: AgentStatusModeEnum;
  consoleConnection: AgentStatusConsoleConnectionEnum;
  loading: boolean;
  initialized: boolean;
  error: string | null;
}

const initialState: AgentState = {
  mode: AgentStatusModeEnum.Disconnected,
  consoleConnection: AgentStatusConsoleConnectionEnum.Disconnected,
  loading: false,
  initialized: false,
  error: null,
};

export const fetchAgentStatus = createAsyncThunk(
  'agent/fetchStatus',
  async () => {
    const response = await apiClient.getAgentStatus();
    return response.data;
  }
);

export const changeAgentMode = createAsyncThunk(
  'agent/changeMode',
  async (mode: AgentModeRequestModeEnum) => {
    const response = await apiClient.setAgentMode({ mode });
    return response.data;
  }
);

const agentSlice = createSlice({
  name: 'agent',
  initialState,
  reducers: {
    setMode: (state, action: PayloadAction<AgentStatusModeEnum>) => {
      state.mode = action.payload;
    },
    setConsoleConnection: (
      state,
      action: PayloadAction<AgentStatusConsoleConnectionEnum>
    ) => {
      state.consoleConnection = action.payload;
    },
    setAgentError: (state, action: PayloadAction<string | null>) => {
      state.error = action.payload;
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchAgentStatus.fulfilled, (state, action) => {
        state.initialized = true;
        if (action.payload) {
          state.mode = action.payload.mode;
          state.consoleConnection = action.payload.console_connection;
          state.error = null;
        }
      })
      .addCase(fetchAgentStatus.rejected, (state, action) => {
        state.initialized = true;
        state.error = capitalizeFirst(action.error.message ?? 'Failed to fetch agent status');
      })
      .addCase(changeAgentMode.pending, (state) => {
        state.loading = true;
      })
      .addCase(changeAgentMode.fulfilled, (state, action) => {
        state.loading = false;
        if (action.payload) {
          state.mode = action.payload.mode;
          state.consoleConnection = action.payload.console_connection;
          state.error = null;
        }
      })
      .addCase(changeAgentMode.rejected, (state, action) => {
        state.loading = false;
        state.error = capitalizeFirst(action.error.message ?? 'Failed to change agent mode');
      });
  },
});

export const { setMode, setConsoleConnection, setAgentError } =
  agentSlice.actions;
export default agentSlice.reducer;

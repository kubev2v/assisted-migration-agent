import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit';
import type { CollectorStartRequest, Inventory } from '@generated/index';
import { CollectorStatusStatusEnum } from '@generated/index';
import { apiClient } from '@shared/api/client';

export interface ApiError {
  code: number | null;
  message: string;
}

function capitalizeFirst(str: string): string {
  if (!str) return str;
  return str.charAt(0).toUpperCase() + str.slice(1);
}

function extractApiError(error: unknown, fallbackMessage: string): ApiError {
  if (error && typeof error === 'object' && 'response' in error) {
    const axiosError = error as { response?: { status?: number; data?: { message?: string } }; message?: string };
    const message = axiosError.response?.data?.message ?? axiosError.message ?? fallbackMessage;
    return {
      code: axiosError.response?.status ?? null,
      message: capitalizeFirst(message),
    };
  }
  if (error instanceof Error) {
    return { code: null, message: capitalizeFirst(error.message) };
  }
  return { code: null, message: capitalizeFirst(fallbackMessage) };
}

interface CollectorState {
  status: CollectorStatusStatusEnum;
  error: ApiError | null;
  inventory: Inventory | null;
  loading: boolean;
  initialized: boolean;
}

const initialState: CollectorState = {
  status: CollectorStatusStatusEnum.Ready,
  error: null,
  inventory: null,
  loading: false,
  initialized: false,
};

export const fetchCollectorStatus = createAsyncThunk(
  'collector/fetchStatus',
  async (_, { rejectWithValue }) => {
    try {
      const response = await apiClient.getCollectorStatus();
      return response.data;
    } catch (error) {
      return rejectWithValue(extractApiError(error, 'Failed to fetch status'));
    }
  }
);

export const startCollection = createAsyncThunk(
  'collector/start',
  async (credentials: CollectorStartRequest, { rejectWithValue }) => {
    try {
      const response = await apiClient.startCollector(credentials);
      return response.data;
    } catch (error) {
      return rejectWithValue(extractApiError(error, 'Failed to start collection'));
    }
  }
);

export const stopCollection = createAsyncThunk(
  'collector/stop',
  async (_, { rejectWithValue }) => {
    try {
      await apiClient.stopCollector();
    } catch (error) {
      return rejectWithValue(extractApiError(error, 'Failed to stop collection'));
    }
  }
);

export const fetchInventory = createAsyncThunk(
  'collector/fetchInventory',
  async (_, { rejectWithValue }) => {
    try {
      const response = await apiClient.getInventory();
      return response.data;
    } catch (error) {
      // 404 means no inventory yet - this is expected, not an error
      if (error && typeof error === 'object' && 'response' in error) {
        const axiosError = error as { response?: { status?: number } };
        if (axiosError.response?.status === 404) {
          return null;
        }
      }
      return rejectWithValue(extractApiError(error, 'Failed to fetch inventory'));
    }
  }
);

const collectorSlice = createSlice({
  name: 'collector',
  initialState,
  reducers: {
    setStatus: (state, action: PayloadAction<CollectorStatusStatusEnum>) => {
      state.status = action.payload;
    },
    setError: (state, action: PayloadAction<ApiError | null>) => {
      state.error = action.payload;
    },
    clearInventory: (state) => {
      state.inventory = null;
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchCollectorStatus.fulfilled, (state, action) => {
        if (action.payload) {
          state.status = action.payload.status;
          state.error = action.payload.error ? { code: null, message: capitalizeFirst(action.payload.error) } : null;
        }
      })
      .addCase(fetchCollectorStatus.rejected, (state, action) => {
        state.status = CollectorStatusStatusEnum.Ready;
        state.error = action.payload as ApiError;
      })
      .addCase(startCollection.pending, (state) => {
        state.loading = true;
        state.error = null;
      })
      .addCase(startCollection.fulfilled, (state, action) => {
        state.loading = false;
        if (action.payload) {
          state.status = action.payload.status;
          state.error = action.payload.error ? { code: null, message: capitalizeFirst(action.payload.error) } : null;
        }
      })
      .addCase(startCollection.rejected, (state, action) => {
        state.loading = false;
        state.status = CollectorStatusStatusEnum.Ready;
        state.error = action.payload as ApiError;
      })
      .addCase(stopCollection.pending, (state) => {
        state.loading = true;
      })
      .addCase(stopCollection.fulfilled, (state) => {
        state.loading = false;
        state.status = CollectorStatusStatusEnum.Ready;
      })
      .addCase(stopCollection.rejected, (state, action) => {
        state.loading = false;
        state.error = action.payload as ApiError;
      })
      .addCase(fetchInventory.fulfilled, (state, action) => {
        state.initialized = true;
        state.inventory = action.payload;
      })
      .addCase(fetchInventory.rejected, (state, action) => {
        state.initialized = true;
        state.error = action.payload as ApiError;
      });
  },
});

export const { setStatus, setError, clearInventory } = collectorSlice.actions;
export default collectorSlice.reducer;

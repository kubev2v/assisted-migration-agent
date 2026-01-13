import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit';
import type { VM, VMDetails, VMListResponse } from '@generated/index';
import { apiClient } from '@shared/api/client';
import type { ApiError } from './collectorSlice';
import { extractApiError } from './collectorSlice';

export interface VMFilters {
  datacenters?: string[];
  clusters?: string[];
  status?: string[];
  issues?: string[];
  diskSizeMin?: number;
  diskSizeMax?: number;
  memorySizeMin?: number;
  memorySizeMax?: number;
  sort?: string[];
}

interface VMState {
  vms: VM[];
  total: number;
  page: number;
  pageSize: number;
  pageCount: number;
  filters: VMFilters;
  sort: string[];
  loading: boolean;
  error: ApiError | null;
  initialized: boolean;
  selectedVMId: string | null;
  selectedVMDetails: VMDetails | null;
  detailLoading: boolean;
  detailError: ApiError | null;
}

const initialState: VMState = {
  vms: [],
  total: 0,
  page: 1,
  pageSize: 20,
  pageCount: 1,
  filters: {},
  sort: [],
  loading: false,
  error: null,
  initialized: false,
  selectedVMId: null,
  selectedVMDetails: null,
  detailLoading: false,
  detailError: null,
};

export interface FetchVMsParams {
  page?: number;
  pageSize?: number;
  filters?: VMFilters;
  sort?: string[];
}

export const fetchVMs = createAsyncThunk(
  'vm/fetchVMs',
  async (params: FetchVMsParams | undefined, { getState, rejectWithValue }) => {
    try {
      const state = getState() as { vm: VMState };
      const page = params?.page ?? state.vm.page;
      const pageSize = params?.pageSize ?? state.vm.pageSize;
      const filters = params?.filters ?? state.vm.filters;
      const sort = params?.sort ?? state.vm.sort;

      const response = await apiClient.getVMs(
        filters.issues,
        filters.datacenters,
        filters.clusters,
        filters.diskSizeMin,
        filters.diskSizeMax,
        filters.memorySizeMin,
        filters.memorySizeMax,
        filters.status,
        sort,
        page,
        pageSize
      );
      return response.data;
    } catch (error) {
      return rejectWithValue(extractApiError(error, 'Failed to fetch VMs'));
    }
  }
);

export const fetchVMDetails = createAsyncThunk(
  'vm/fetchVMDetails',
  async (id: string, { rejectWithValue }) => {
    try {
      const response = await apiClient.getVM(id);
      return response.data;
    } catch (error) {
      return rejectWithValue(extractApiError(error, 'Failed to fetch VM details'));
    }
  }
);

const vmSlice = createSlice({
  name: 'vm',
  initialState,
  reducers: {
    setPage: (state, action: PayloadAction<number>) => {
      state.page = action.payload;
    },
    setPageSize: (state, action: PayloadAction<number>) => {
      state.pageSize = action.payload;
      state.page = 1; // Reset to first page when changing page size
    },
    setFilters: (state, action: PayloadAction<VMFilters>) => {
      state.filters = action.payload;
      state.page = 1; // Reset to first page when changing filters
    },
    setSort: (state, action: PayloadAction<string[]>) => {
      state.sort = action.payload;
    },
    clearError: (state) => {
      state.error = null;
    },
    selectVM: (state, action: PayloadAction<string>) => {
      state.selectedVMId = action.payload;
      state.selectedVMDetails = null;
      state.detailError = null;
    },
    clearSelectedVM: (state) => {
      state.selectedVMId = null;
      state.selectedVMDetails = null;
      state.detailError = null;
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchVMs.pending, (state) => {
        state.loading = true;
        state.error = null;
      })
      .addCase(fetchVMs.fulfilled, (state, action: PayloadAction<VMListResponse>) => {
        state.loading = false;
        state.initialized = true;
        state.vms = action.payload.vms;
        state.total = action.payload.total;
        state.page = action.payload.page;
        state.pageCount = action.payload.pageCount;
      })
      .addCase(fetchVMs.rejected, (state, action) => {
        state.loading = false;
        state.initialized = true;
        state.error = action.payload as ApiError;
      })
      .addCase(fetchVMDetails.pending, (state) => {
        state.detailLoading = true;
        state.detailError = null;
      })
      .addCase(fetchVMDetails.fulfilled, (state, action: PayloadAction<VMDetails>) => {
        state.detailLoading = false;
        state.selectedVMDetails = action.payload;
      })
      .addCase(fetchVMDetails.rejected, (state, action) => {
        state.detailLoading = false;
        state.detailError = action.payload as ApiError;
      });
  },
});

export const { setPage, setPageSize, setFilters, setSort, clearError, selectVM, clearSelectedVM } = vmSlice.actions;
export default vmSlice.reducer;

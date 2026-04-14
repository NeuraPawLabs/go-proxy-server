import api from './index';
import type { User, AddUserRequest, DeleteUserRequest } from '../types/user';

export const getUsers = () => api.get<User[]>('/users');

export const addUser = (data: AddUserRequest) =>
  api.post('/users', data);

export const deleteUser = (data: DeleteUserRequest) =>
  api.delete('/users', { data });
